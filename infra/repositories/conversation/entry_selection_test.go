package conversation_repository

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/conversation"
	"vozko/domain/crmfilter"
	"vozko/domain/selection"
	"vozko/domain/shared"
)

func selectionInput() conversation.SearchByFilterInput {
	return conversation.SearchByFilterInput{
		WorkspaceID: "ws-1",
		Filter: crmfilter.Filter{Groups: []crmfilter.Group{{
			Conjunction: crmfilter.And,
			Predicates:  []crmfilter.Predicate{{Field: crmfilter.FieldUnread, Operator: crmfilter.OpIsTrue}},
		}}},
	}
}

func TestFilteredEntriesCTE_PlaceholdersMatchArgs(t *testing.T) {
	in := selectionInput()
	in.ExcludeEntryIDs = []string{"e-9", "e-8"}
	in.RestrictDepartments = true
	in.DepartmentIDs = []string{"d-1"}
	sql, args, err := filteredEntriesCTE(in)
	if err != nil {
		t.Fatal(err)
	}
	assertPlaceholdersMatchArgs(t, sql, args)
	if !strings.Contains(sql, "ae.entry_id::text = ANY(?)") {
		t.Fatalf("exclusions must bind one array:\n%s", sql)
	}
}

func TestFilteredEntriesCTE_RequiresWorkspace(t *testing.T) {
	if _, _, err := filteredEntriesCTE(conversation.SearchByFilterInput{}); err == nil {
		t.Fatal("a missing workspace must be refused")
	}
}

func TestEntryRefPageQuery_IsAPlainKeysetRead(t *testing.T) {
	in := selectionInput()
	in.ExcludeEntryIDs = []string{"e-9"}
	in.RestrictDepartments = true
	in.DepartmentIDs = []string{"d-1"}
	in.AssigneeOverrideUserID = "u-1"

	sql, args, err := entryRefPageQuery(in, "a0", 2)
	if err != nil {
		t.Fatal(err)
	}
	assertPlaceholdersMatchArgs(t, sql, args)
	if tail := sql[strings.LastIndex(sql, "SELECT ewm.entry_id"):]; strings.Contains(tail, "OVER (") || strings.Contains(tail, "COUNT(") {
		t.Fatalf("a page never counts the whole selection:\n%s", sql)
	}
	if !strings.Contains(sql, "ae.entry_id::text > ?") {
		t.Fatalf("the keyset bound must filter inside the union, before the join:\n%s", sql)
	}
	if !strings.HasSuffix(strings.TrimSpace(sql), "ORDER BY ewm.entry_id::text LIMIT ?") {
		t.Fatalf("the page must be ordered by the keyset column:\n%s", sql)
	}
	if args[len(args)-1] != 2 || args[len(args)-2] != "a0" {
		t.Fatalf("keyset args must close the list, got %v", args[len(args)-2:])
	}
}

func TestResolveEntryRefsByFilter_ReturnsOneKeysetPage(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`(?s)ae\.entry_id::text > \$\d+.*SELECT ewm\.entry_id::text AS entry_id, ewm\.entry_type FROM entries_with_msg ewm ORDER BY ewm\.entry_id::text LIMIT \$\d+`).
		WillReturnRows(sqlmock.NewRows([]string{"entry_id", "entry_type"}).
			AddRow("a1", "whatsapp_campaign").
			AddRow("a2", "unofficial_whatsapp_campaign"))

	refs, err := (&repository{db: db}).ResolveEntryRefsByFilter(selectionInput(), "a0", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[1] != (shared.EntryRef{EntryID: "a2", EntryType: shared.EntryType("unofficial_whatsapp_campaign")}) {
		t.Fatalf("unexpected refs %+v", refs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestResolveEntryRefsByFilter_EmptyPageIsEmpty(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`ORDER BY ewm\.entry_id::text LIMIT`).
		WillReturnRows(sqlmock.NewRows([]string{"entry_id", "entry_type"}))

	refs, err := (&repository{db: db}).ResolveEntryRefsByFilter(selectionInput(), "zz", 500)
	if err != nil || refs == nil || len(refs) != 0 {
		t.Fatalf("got %v, %v", refs, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestResolveEntryRefsByFilter_RefusesAnUnboundedPage(t *testing.T) {
	for _, limit := range []int{0, -1, selection.MaxResolvePage + 1} {
		if _, err := (&repository{}).ResolveEntryRefsByFilter(selectionInput(), "", limit); err == nil {
			t.Errorf("limit %d must be refused", limit)
		}
	}
}

func TestResolveEntryRefsByFilter_SurfacesDatabaseErrors(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	boom := errors.New("db down")
	mock.ExpectQuery(`ORDER BY ewm\.entry_id::text LIMIT`).WillReturnError(boom)

	if _, err := (&repository{db: db}).ResolveEntryRefsByFilter(selectionInput(), "", 10); !errors.Is(err, boom) {
		t.Fatalf("want the database error, got %v", err)
	}
}

func TestCountEntriesByFilter(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM entries_with_msg`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))

	got, err := (&repository{db: db}).CountEntriesByFilter(selectionInput())
	if err != nil || got != 42 {
		t.Fatalf("got %d, %v", got, err)
	}
}

func TestEntryRefPageQuery_TheFirstReadHasNoKeysetBound(t *testing.T) {
	sql, args, err := entryRefPageQuery(selectionInput(), "", 2001)
	if err != nil {
		t.Fatal(err)
	}
	assertPlaceholdersMatchArgs(t, sql, args)
	if strings.Contains(sql, "ae.entry_id::text > ?") {
		t.Fatalf("a read from the start carries no keyset bound:\n%s", sql)
	}
	if !strings.HasSuffix(strings.TrimSpace(sql), "ORDER BY ewm.entry_id::text LIMIT ?") || args[len(args)-1] != 2001 {
		t.Fatalf("the read must be ordered and bounded by the limit, got %v", args[len(args)-1])
	}
}

func TestResolveEntryRefsByFilter_ReadsAWholeBulkSetInOneStatement(t *testing.T) {
	db, mock, sqlDB := newStatusDB(t)
	defer sqlDB.Close()

	rows := sqlmock.NewRows([]string{"entry_id", "entry_type"})
	for i := range 2001 {
		rows.AddRow(fmt.Sprintf("e%05d", i), "whatsapp_campaign")
	}
	mock.ExpectQuery(`(?s)^\s*WITH all_entries AS .*SELECT ewm\.entry_id::text AS entry_id, ewm\.entry_type FROM entries_with_msg ewm ORDER BY ewm\.entry_id::text LIMIT \$\d+$`).
		WithArgs(boundLimit(t, 2001)...).
		WillReturnRows(rows)

	refs, err := (&repository{db: db}).ResolveEntryRefsByFilter(selectionInput(), "", 2001)
	if err != nil || len(refs) != 2001 {
		t.Fatalf("got %d refs, %v", len(refs), err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type limitArg int

func (l limitArg) Match(v driver.Value) bool {
	n, ok := v.(int64)
	return ok && n == int64(l)
}

func boundLimit(t *testing.T, limit int) []driver.Value {
	t.Helper()
	_, args, err := entryRefPageQuery(selectionInput(), "", limit)
	if err != nil {
		t.Fatal(err)
	}
	matchers := make([]driver.Value, len(args))
	for i := range matchers {
		matchers[i] = sqlmock.AnyArg()
	}
	matchers[len(matchers)-1] = limitArg(limit)
	return matchers
}
