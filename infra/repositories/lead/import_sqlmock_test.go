package lead

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"vozko/domain/lead"
	"vozko/domain/leadimport"
)

const (
	importUUID = "2a3b4c5d-6e7f-4a8b-9c0d-1e2f3a4b5c6d"
	claimToken = "claim-1"
)

func args(values ...[]driver.Value) []driver.Value {
	var out []driver.Value
	for _, v := range values {
		out = append(out, v...)
	}
	return out
}

func claimedImport() *leadimport.Job {
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	return &leadimport.Job{ID: importUUID, WorkspaceID: wsUUID, RequestedBy: actorUUID, Status: leadimport.StatusImporting, Stage: leadimport.StageRows,
		Claim: claimToken, Attempts: 1, HeartbeatAt: &now, Result: &leadimport.Counts{}, Settings: &leadimport.Settings{Policy: lead.PolicyFillEmpty},
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(leadimport.Retention)}
}

func TestImportsSaveBindsTheStatusGuardAndTheClaim(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	imports := &Imports{r: &repository{db: db}}
	job := claimedImport()

	mock.ExpectQuery(exact(importSaveSQL + " RETURNING id")).
		WithArgs(args([]driver.Value{"importing"}, anyArgs(16), []driver.Value{importUUID, pq.StringArray{"analyzed"}})...).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(importUUID))
	mock.ExpectQuery(exact(importSaveSQL + importClaimGuardSQL + " RETURNING id")).
		WithArgs(args([]driver.Value{"importing"}, anyArgs(16), []driver.Value{importUUID, pq.StringArray{"importing"}, claimToken})...).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	if err := imports.Save(context.Background(), job, leadimport.Guard{From: []leadimport.Status{leadimport.StatusAnalyzed}}); err != nil {
		t.Fatalf("Save = %v", err)
	}
	err := imports.Save(context.Background(), job, leadimport.Guard{From: []leadimport.Status{leadimport.StatusImporting}, Claim: claimToken})
	if !errors.Is(err, leadimport.ErrClaimLost) {
		t.Fatalf("Save under a lost claim = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestImportsClaimBindsTheAttemptCapAndTheStaleWindow(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	imports := &Imports{r: &repository{db: db}}
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(exact(importClaimSQL)).
		WithArgs(claimToken, now, now, importUUID, leadimport.MaxAttempts, now.Add(-leadimport.StaleAfter)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "status", "claim_token", "attempts"}).AddRow(importUUID, wsUUID, "importing", claimToken, 1))
	job, err := imports.Claim(context.Background(), importUUID, claimToken, now)
	if err != nil || job.Claim != claimToken || job.Status != leadimport.StatusImporting {
		t.Fatalf("Claim = %+v, %v", job, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHoldingPhonesAsksForOneHolderPastTheCap(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	imports := &Imports{r: &repository{db: db}}
	formats := pq.StringArray{"551133334444"}
	expect := func(rows int) {
		result := sqlmock.NewRows([]string{"id", "workspace_id"})
		for i := 0; i < rows; i++ {
			result.AddRow(leadUUID, wsUUID)
		}
		mock.ExpectQuery(exact(importHoldersSQL)).WithArgs(wsUUID, wsUUID, formats, wsUUID, formats, leadimport.MaxContactHolders+1).WillReturnRows(result)
	}
	expect(0)
	expect(leadimport.MaxContactHolders + 1)
	if holders, err := imports.HoldingPhones(context.Background(), wsUUID, []string{"551133334444"}); err != nil || len(holders) != 0 {
		t.Fatalf("HoldingPhones = %v, %v", holders, err)
	}
	if _, err := imports.HoldingPhones(context.Background(), wsUUID, []string{"551133334444"}); !errors.Is(err, leadimport.ErrTooManyHolders) {
		t.Fatalf("HoldingPhones past the cap = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWriteRowsLocksEnrichesAndCheckpointsWithEveryArgument(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	imports := &Imports{r: &repository{db: db}}
	job := claimedImport()
	existing := &lead.Lead{ID: leadUUID, WorkspaceID: wsUUID, Number: "5511987654321", Name: "Maria", NameSource: lead.SourceManual, Version: 3}
	existing.EnsureCollections()
	row := lead.ImportRecord{WorkspaceID: wsUUID, Line: 2, Number: "5511987654321", Email: "maria@example.com"}

	mock.ExpectBegin()
	mock.ExpectQuery(exact(importLockSQL)).WithArgs(wsUUID, pq.StringArray{leadUUID}).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version"}).AddRow(leadUUID, 3))
	mock.ExpectQuery(exact(importEnrichSQL)).
		WithArgs(args([]driver.Value{sqlmock.AnyArg(), pq.StringArray{leadUUID}, pq.Int64Array{3}, pq.StringArray{"Maria"}}, anyArgs(10), []driver.Value{wsUUID})...).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version"}).AddRow(leadUUID, 4))
	mock.ExpectExec(`INSERT INTO "lead_events"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(exact(importCheckpointSQL)).WithArgs(1, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), importUUID, claimToken).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(importUUID))
	mock.ExpectCommit()

	batch, outcomes := rowBatch(job, []leadimport.RowItem{{Record: row, Existing: existing}}, 1, nil)
	if err := imports.WriteRows(context.Background(), batch); err != nil {
		t.Fatalf("WriteRows = %v", err)
	}
	if got := (*outcomes)[0]; got.Decision.Verdict != lead.ImportEnriched || got.Decision.Lead.Version != 4 {
		t.Fatalf("outcome = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWriteLinksReportsAPairThatWasAlreadyLinked(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	imports := &Imports{r: &repository{db: db, newID: func() string { return relationUUID }}}
	job := claimedImport()
	rel, err := lead.NewRelation(otherUUID, leadUUID, lead.KindChild, actorUUID)
	if err != nil {
		t.Fatal(err)
	}
	link := leadimport.PendingLink{ID: "5e6f7a8b-9c0d-4e1f-8a2b-3c4d5e6f7a8b", Line: 7, LeadID: leadUUID, Kind: lead.KindChild}

	mock.ExpectBegin()
	mock.ExpectQuery(exact(lockLeadsSQL)).WithArgs(wsUUID, pq.StringArray{otherUUID, leadUUID}).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(otherUUID).AddRow(leadUUID))
	mock.ExpectQuery(exact(importInsertRelationsSQL)).
		WithArgs(wsUUID, sqlmock.AnyArg(), pq.StringArray{relationUUID}, pq.StringArray{rel.LeadID}, pq.StringArray{rel.OtherLeadID},
			pq.StringArray{"family"}, pq.StringArray{string(rel.Kind)}, pq.StringArray{actorUUID}).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`INSERT INTO "lead_import_issues"`).WithArgs(importUUID, 7, "relation_exists", "relative_number", false).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(exact(importDeleteLinksSQL)).WithArgs(importUUID, pq.StringArray{link.ID}).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(exact(importCheckpointSQL)).WithArgs(0, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), importUUID, claimToken).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(importUUID))
	mock.ExpectCommit()

	var failed []lead.ImportIssue
	err = imports.WriteLinks(context.Background(), leadimport.LinkBatch{Job: job, Links: []leadimport.LinkWrite{{Link: link, Relation: &rel}},
		Checkpoint: func(created int, issues []lead.ImportIssue) leadimport.Counts {
			failed = issues
			return leadimport.Counts{LinksCreated: created}
		}})
	if err != nil {
		t.Fatalf("WriteLinks = %v", err)
	}
	if len(failed) != 1 || failed[0].Reason != lead.ReasonRelationExists || failed[0].Line != 7 {
		t.Fatalf("failed = %+v", failed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPhoneHolderCountsReadsOnlyCappedCounts(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	imports := &Imports{r: &repository{db: db}}
	formats := pq.StringArray{"551133334444", "5511987650000", "551187650000"}
	mock.ExpectQuery(exact(importHolderCountsSQL)).
		WithArgs(wsUUID, wsUUID, leadimport.MaxContactHolders+1, formats).
		WillReturnRows(sqlmock.NewRows([]string{"number", "holders"}).AddRow("551133334444", leadimport.MaxContactHolders+1).AddRow("5511987650000", 2).AddRow("551187650000", 0))
	counts, err := imports.PhoneHolderCounts(context.Background(), wsUUID, []string{"551133334444", "5511987650000", "551133334444"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"551133334444": leadimport.MaxContactHolders + 1, "5511987650000": 2, "551187650000": 0}
	if len(counts) != len(want) {
		t.Fatalf("counts = %v", counts)
	}
	for number, n := range want {
		if counts[number] != n {
			t.Fatalf("counts = %v, want %v", counts, want)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if got := countPlaceholders(importHolderCountsSQL); got != 4 {
		t.Fatalf("placeholders = %d, want 4", got)
	}
}

func TestMineReadsTheImportersLiveImportsNewestFirst(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	imports := &Imports{r: &repository{db: db}}
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(exact(importMineSQL)).WithArgs(wsUUID, actorUUID, now, leadimport.MaxListed).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "requested_by", "status"}).AddRow(importUUID, wsUUID, actorUUID, "done"))
	jobs, err := imports.Mine(context.Background(), wsUUID, actorUUID, now, leadimport.MaxListed)
	if err != nil || len(jobs) != 1 || jobs[0].ID != importUUID || jobs[0].Status != leadimport.StatusDone {
		t.Fatalf("Mine = %+v, %v", jobs, err)
	}
	if _, err := imports.Mine(context.Background(), wsUUID, " ", now, leadimport.MaxListed); !errors.Is(err, leadimport.ErrNotFound) {
		t.Fatalf("Mine without a requester = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPlacementCountsTheAddressesTheImportAdded(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	imports := &Imports{r: &repository{db: db}}
	bound := make([]driver.Value, 0, len(importPlacementArgs)+2)
	for _, a := range importPlacementArgs {
		bound = append(bound, a)
	}

	mock.ExpectQuery(exact(importPlacementSQL)).
		WithArgs(append(bound, wsUUID, importUUID)...).
		WillReturnRows(sqlmock.NewRows([]string{"on_map", "approximate", "not_found", "quota_exceeded", "refused", "pending"}).AddRow(3088, 1920, 12, 4, 1, 709))
	got, err := imports.Placement(context.Background(), wsUUID, importUUID)
	if err != nil || got != (leadimport.Placement{OnMap: 3088, Approximate: 1920, NotFound: 12, QuotaExceeded: 4, Refused: 1, Pending: 709}) {
		t.Fatalf("Placement = %+v, %v", got, err)
	}
	if _, err := imports.Placement(context.Background(), wsUUID, "not-a-uuid"); !errors.Is(err, leadimport.ErrNotFound) {
		t.Fatalf("Placement of a malformed id = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFailStalledReportsEachImportWithTheStatusItStalledIn(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	imports := &Imports{r: &repository{db: db}}
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(exact(importFailStalledSQL)).WithArgs(leadimport.MaxAttempts, now.Add(-leadimport.StaleAfter), now, now).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "status"}).AddRow(importUUID, wsUUID, "importing").AddRow(leadUUID, wsUUID, "analyzing"))
	got, err := imports.FailStalled(context.Background(), now)
	want := []leadimport.Stalled{
		{Ref: leadimport.Ref{ID: importUUID, WorkspaceID: wsUUID}, Status: leadimport.StatusImporting},
		{Ref: leadimport.Ref{ID: leadUUID, WorkspaceID: wsUUID}, Status: leadimport.StatusAnalyzing},
	}
	if err != nil || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("FailStalled = %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimableNamesTheWorkspaceOfEachImport(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	imports := &Imports{r: &repository{db: db}}
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(exact(importClaimableSQL)).WithArgs(leadimport.MaxAttempts, now.Add(-leadimport.StaleAfter), 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id"}).AddRow(importUUID, wsUUID))
	got, err := imports.Claimable(context.Background(), now, 20)
	if err != nil || len(got) != 1 || got[0] != (leadimport.Ref{ID: importUUID, WorkspaceID: wsUUID}) {
		t.Fatalf("Claimable = %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPlacementCountsLiveLeadsByTheirPrimaryAddressLikeTheMap(t *testing.T) {
	for _, part := range []string{"JOIN leads ON leads.id = la.lead_id AND leads.workspace_id = la.workspace_id AND leads.deleted_at IS NULL", "la.is_primary", "la.import_id = ?"} {
		if !strings.Contains(importPlacementSQL, part) {
			t.Errorf("placement SQL misses %q:\n%s", part, importPlacementSQL)
		}
	}
}
