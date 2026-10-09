package lead

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

const snapshotID = "5c1e2d3f-4a5b-4c6d-8e7f-9a0b1c2d3e4f"

func blockedSelection() crmfilter.Filter {
	return crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsTrue}}}}}
}

func notBlocked() crmfilter.Filter {
	return crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsFalse}}}}}
}

func TestSelectionStatements_BindEveryPlaceholder(t *testing.T) {
	r := &repository{}
	queries := map[string]lead.SelectionQuery{
		"workspace":       {WorkspaceID: pageWorkspace},
		"picked":          {WorkspaceID: pageWorkspace, IDs: []string{firstLead, secondLead}},
		"filtered":        {WorkspaceID: pageWorkspace, Filter: blockedSelection(), ExcludeIDs: []string{firstLead}, Require: notBlocked()},
		"first n ordered": {WorkspaceID: pageWorkspace, Filter: blockedSelection(), Order: []shared.Sort{lead.DefaultSort}, Limit: 10, ExcludeIDs: []string{firstLead}},
		"pending field":   {WorkspaceID: pageWorkspace, Filter: blockedSelection(), Order: []shared.Sort{lead.DefaultSort}, Limit: 10, Pending: &lead.Assignment{Kind: lead.AssignCustomField, Key: "interesse", Value: "alto"}},
		"pending owner":   {WorkspaceID: pageWorkspace, Filter: blockedSelection(), Order: []shared.Sort{lead.DefaultSort}, Limit: 10, Pending: &lead.Assignment{Kind: lead.AssignOwner, Value: "ai:" + firstLead}},
		"pending block":   {WorkspaceID: pageWorkspace, Filter: blockedSelection(), Order: []shared.Sort{lead.DefaultSort}, Limit: 10, Pending: &lead.Assignment{Kind: lead.AssignBlocked, Value: true}},
	}
	for name, sq := range queries {
		s, err := r.selection(sq)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for label, stmt := range map[string]func() (string, []interface{}){
			"count":  s.countSQL,
			"page":   func() (string, []interface{}) { return s.pageSQL("", 500) },
			"after":  func() (string, []interface{}) { return s.pageSQL(firstLead, 500) },
			"freeze": func() (string, []interface{}) { return s.freezeSQL(snapshotID, time.Unix(0, 0)) },
		} {
			sql, args := stmt()
			assertPlaceholders(t, name+" "+label, sql, args)
		}
	}
}

func TestCountSelection_IsUncachedAndBoundToThePickedIDs(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}

	for i := 0; i < 2; i++ {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("SET LOCAL jit = off")).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta("SET LOCAL statement_timeout = '30s'")).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM leads WHERE leads.workspace_id = $1 AND leads.deleted_at IS NULL AND leads.id = ANY($2::uuid[])")).
			WithArgs(pageWorkspace, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
		mock.ExpectCommit()
	}

	for i := 0; i < 2; i++ {
		n, err := r.CountSelection(context.Background(), lead.SelectionQuery{WorkspaceID: pageWorkspace, IDs: []string{firstLead, secondLead}})
		if err != nil || n != 2 {
			t.Fatalf("CountSelection = %d, %v", n, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSelectionPage_KeysetsOnTheWorkspaceAndID(t *testing.T) {
	r := &repository{}
	s, err := r.selection(lead.SelectionQuery{WorkspaceID: pageWorkspace, Filter: blockedSelection(), ExcludeIDs: []string{firstLead}, Require: notBlocked()})
	if err != nil {
		t.Fatal(err)
	}
	sql, args := s.pageSQL(secondLead, 500)
	want := "SELECT leads.id FROM leads WHERE leads.workspace_id = ? AND leads.deleted_at IS NULL AND (" +
		"(leads.blocked = true)) AND NOT (leads.id = ANY(?::uuid[])) AND ((leads.blocked = false)) AND leads.id > ?::uuid ORDER BY leads.id LIMIT ?"
	if sql != want {
		t.Fatalf("page SQL\n got: %s\nwant: %s", sql, want)
	}
	if args[len(args)-1] != 500 || args[len(args)-2] != secondLead {
		t.Fatalf("page args end with %v", args[len(args)-2:])
	}
}

func TestSelectionPage_FirstNPicksTheOrderedHeadThenKeysets(t *testing.T) {
	r := &repository{}
	s, err := r.selection(lead.SelectionQuery{
		WorkspaceID: pageWorkspace, Filter: blockedSelection(), ExcludeIDs: []string{firstLead}, Require: notBlocked(),
		Order: []shared.Sort{{Field: string(lead.SortName), Direction: shared.SortAsc}}, Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	sql, args := s.pageSQL(secondLead, 20)
	if !strings.HasPrefix(sql, "SELECT chosen.id FROM (SELECT leads.id FROM leads WHERE ") ||
		!strings.Contains(sql, "AND NOT (leads.id = ANY(?::uuid[])) AND ((leads.blocked = false)) ORDER BY NULLIF(leads.name, '') ASC NULLS LAST, leads.id DESC LIMIT ?) chosen WHERE chosen.id > ?::uuid ORDER BY chosen.id LIMIT ?") {
		t.Fatalf("first n page SQL: %s", sql)
	}
	if got := args[len(args)-3:]; got[0] != 50 || got[1] != secondLead || got[2] != 20 {
		t.Fatalf("first n page args end with %v", got)
	}
}

func TestFreezeSelection_LocksTheSnapshotAndInsertsTheSetOnce(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}
	q := lead.SelectionQuery{WorkspaceID: pageWorkspace, Filter: blockedSelection(), Order: []shared.Sort{lead.DefaultSort}, Limit: 3}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SET LOCAL jit = off")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("SET LOCAL statement_timeout = '60s'")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))")).WithArgs(snapshotID).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("^"+regexp.QuoteMeta("SELECT COUNT(*) FROM lead_selection_snapshots WHERE snapshot_id = $1::uuid AND workspace_id = $2::uuid")+"$").WithArgs(snapshotID, pageWorkspace).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(`^INSERT INTO lead_selection_snapshots \(snapshot_id, workspace_id, lead_id, created_at\) SELECT \$1::uuid, leads\.workspace_id, leads\.id, \$2 FROM leads WHERE leads\.workspace_id = \$3 .* ORDER BY leads\.created_at DESC NULLS LAST, leads\.id DESC LIMIT \$4 ON CONFLICT DO NOTHING$`).
		WithArgs(snapshotID, sqlmock.AnyArg(), pageWorkspace, 3).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	frozen, err := r.FreezeSelection(context.Background(), q, snapshotID)
	if err != nil || frozen.Size != 3 || frozen.Existed {
		t.Fatalf("FreezeSelection = %+v, %v", frozen, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFreezeSelection_AnExistingSnapshotIsKept(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SET LOCAL jit = off")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("SET LOCAL statement_timeout = '60s'")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))")).WithArgs(snapshotID).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("^"+regexp.QuoteMeta("SELECT COUNT(*) FROM lead_selection_snapshots WHERE snapshot_id = $1::uuid AND workspace_id = $2::uuid")+"$").WithArgs(snapshotID, pageWorkspace).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))
	mock.ExpectCommit()

	frozen, err := r.FreezeSelection(context.Background(), lead.SelectionQuery{WorkspaceID: pageWorkspace}, snapshotID)
	if err != nil || frozen.Size != 7 || !frozen.Existed {
		t.Fatalf("FreezeSelection = %+v, %v", frozen, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFreezeSelection_RefusesAMalformedSnapshot(t *testing.T) {
	r := &repository{}
	if _, err := r.FreezeSelection(context.Background(), lead.SelectionQuery{WorkspaceID: pageWorkspace}, "snap"); err == nil {
		t.Fatal("a snapshot id that is not a uuid was accepted")
	}
}

func TestSnapshotPage_KeysetsOverTheFrozenSet(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT lead_id FROM lead_selection_snapshots WHERE snapshot_id = $1::uuid AND workspace_id = $2::uuid AND lead_id > $3::uuid ORDER BY lead_id LIMIT $4")).
		WithArgs(snapshotID, pageWorkspace, firstLead, 500).
		WillReturnRows(sqlmock.NewRows([]string{"lead_id"}).AddRow(secondLead))

	ids, err := r.SnapshotPage(context.Background(), pageWorkspace, snapshotID, firstLead, 500)
	if err != nil || len(ids) != 1 || ids[0] != secondLead {
		t.Fatalf("SnapshotPage = %v, %v", ids, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDropSnapshotsBefore_DeletesInBoundedBatches(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}
	before := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM lead_selection_snapshots WHERE ctid IN (SELECT kept.ctid FROM lead_selection_snapshots kept WHERE kept.created_at < $1"+
		" AND NOT EXISTS (SELECT 1 FROM lead_action_runs r WHERE r.id = kept.snapshot_id AND r.status IN ('queued', 'running'))"+
		" AND NOT EXISTS (SELECT 1 FROM report_jobs j WHERE j.workspace_id = kept.workspace_id AND j.kind = 'leads' AND j.status IN ('queued', 'running') AND j.params ->> 'snapshotId' = kept.snapshot_id::text)"+
		" AND NOT EXISTS (SELECT 1 FROM call_lists cl WHERE cl.id = kept.snapshot_id AND cl.status = 'building')"+
		" LIMIT $2)")).
		WithArgs(before, 5000).
		WillReturnResult(sqlmock.NewResult(0, 120))

	n, err := r.DropSnapshotsBefore(context.Background(), before, 5000)
	if err != nil || n != 120 {
		t.Fatalf("DropSnapshotsBefore = %d, %v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAQuantitySelectionSkipsLeadsThatAlreadyHoldTheAssignmentBeforeItsLimit(t *testing.T) {
	r := &repository{}
	s, err := r.selection(lead.SelectionQuery{WorkspaceID: pageWorkspace, Order: []shared.Sort{lead.DefaultSort}, Limit: 10, Pending: &lead.Assignment{Kind: lead.AssignBlocked, Value: true}})
	if err != nil {
		t.Fatal(err)
	}
	sql, args := s.freezeSQL(snapshotID, time.Unix(0, 0))
	pending := strings.Index(sql, "AND NOT (leads.blocked = ?)")
	if pending < 0 || pending > strings.Index(sql, "ORDER BY") {
		t.Fatalf("the skip is not applied before the limit: %s", sql)
	}
	assertPlaceholders(t, "pending freeze", sql, args)
	if _, err := r.selection(lead.SelectionQuery{WorkspaceID: pageWorkspace, Pending: &lead.Assignment{Kind: "stage"}}); !errors.Is(err, lead.ErrAssignmentInvalid) {
		t.Fatalf("an unknown assignment = %v", err)
	}
}

func TestSnapshotSize_CountsTheFrozenSet(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM lead_selection_snapshots WHERE snapshot_id = $1::uuid AND workspace_id = $2::uuid")).
		WithArgs(snapshotID, pageWorkspace).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))
	n, err := r.SnapshotSize(context.Background(), pageWorkspace, snapshotID)
	if err != nil || n != 7 {
		t.Fatalf("SnapshotSize = %d, %v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
