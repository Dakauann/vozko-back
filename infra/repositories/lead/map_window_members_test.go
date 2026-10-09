package lead

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/crmfilter"
	"vozko/domain/leadmap"
)

func TestAWindowCountsItsRowsOnlyUpToOnePastTheManyRowsMark(t *testing.T) {
	m := compiledMap(t)
	q := layerRequest(t, "")
	sql, args := m.windowRows(q.Claim)
	want := "SELECT count(*) AS window_rows FROM (SELECT 1 FROM lead_addresses a WHERE a.workspace_id = ? AND a.is_primary AND a.latitude IS NOT NULL" +
		" AND a.latitude > ? AND a.latitude <= ? AND a.longitude >= ? AND a.longitude < ? LIMIT ?) w"
	if sql != want {
		t.Fatalf("window rows SQL\n got: %s\nwant: %s", sql, want)
	}
	if strings.Count(sql, "?") != len(args) || args[0] != mapWorkspace || args[len(args)-1] != manyWindowRows+1 {
		t.Fatalf("args = %v", args)
	}
}

var (
	windowRowsSQL  = regexp.QuoteMeta("SELECT count(*) AS window_rows FROM (SELECT 1 FROM lead_addresses a")
	membersOnceSQL = regexp.QuoteMeta("SELECT count(*) AS positions FROM (") + ".*" + regexp.QuoteMeta("AND (a.lead_id IN (SELECT leads.id FROM leads WHERE ") + ".*" + regexp.QuoteMeta(")) IS TRUE GROUP BY")
	membersProbed  = regexp.QuoteMeta("SELECT count(*) AS positions FROM (") + ".*" + regexp.QuoteMeta("AND a.lead_id IN (SELECT leads.id FROM leads WHERE ") + ".*" + regexp.QuoteMeta(")) GROUP BY")
)

func windowRowsOf(n int) *sqlmock.Rows { return sqlmock.NewRows([]string{"window_rows"}).AddRow(n) }

func noPositions() *sqlmock.Rows { return sqlmock.NewRows([]string{"positions"}).AddRow(0) }

func TestAFilteredWindowWithManyRowsReadsItsMembersOnceAndASmallOneProbesThem(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	expectMapSession(mock)
	mock.ExpectQuery(windowRowsSQL).WillReturnRows(windowRowsOf(manyWindowRows + 1))
	mock.ExpectQuery(membersOnceSQL).WillReturnRows(noPositions())
	mock.ExpectCommit()
	expectMapSession(mock)
	mock.ExpectQuery(windowRowsSQL).WillReturnRows(windowRowsOf(manyWindowRows))
	mock.ExpectQuery(membersProbed).WillReturnRows(noPositions())
	mock.ExpectCommit()

	reader := NewMapReader(db)
	scope := leadmap.Scope{WorkspaceID: mapWorkspace, Filter: blockedFilter()}
	for i := 0; i < 2; i++ {
		if _, err := reader.Layer(context.Background(), scope, layerRequest(t, "")); err != nil {
			t.Fatal(err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestASearchedWindowReadsItsMembersOnceHoweverSmall(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	expectMapSession(mock)
	mock.ExpectQuery(windowRowsSQL).WillReturnRows(windowRowsOf(3))
	mock.ExpectQuery(membersOnceSQL).WillReturnRows(noPositions())
	mock.ExpectCommit()
	searched := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldQuery, Operator: crmfilter.OpContains, Values: []string{"maria silva"}},
	}}}}
	if _, err := NewMapReader(db).Layer(context.Background(), leadmap.Scope{WorkspaceID: mapWorkspace, Filter: searched}, layerRequest(t, "")); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAWindowWithoutAFilterSkipsTheRowCountAndProbesTheLeads(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	expectMapSession(mock)
	mock.ExpectQuery(regexp.QuoteMeta("AND a.lead_id IN (SELECT leads.id FROM leads WHERE leads.workspace_id = ") + `\$\d+` + regexp.QuoteMeta(" AND leads.deleted_at IS NULL) GROUP BY")).WillReturnRows(noPositions())
	mock.ExpectCommit()
	if _, err := NewMapReader(db).Layer(context.Background(), leadmap.Scope{WorkspaceID: mapWorkspace}, layerRequest(t, "")); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAnAreaWindowAlreadyHashesItsMembersSoItSkipsTheRowCount(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	expectAreaSession(mock)
	mock.ExpectQuery(membersProbed).WillReturnRows(noPositions())
	mock.ExpectCommit()
	if _, err := NewMapReader(db).Layer(context.Background(), leadmap.Scope{WorkspaceID: mapWorkspace, Filter: boundAreaFilter()}, layerRequest(t, "")); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
