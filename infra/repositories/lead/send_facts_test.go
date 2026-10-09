package lead

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"vozko/domain/campaign"
	"vozko/infra/database"
)

func TestLeadFactsReadsTheWholeChunkInOneArrayBoundStatement(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	known, blocked, quiet := uuid.NewString(), uuid.NewString(), uuid.NewString()
	ids := []string{known, blocked, quiet, "not-a-uuid", known}
	mock.ExpectQuery(exact(leadFactsSQL)).
		WithArgs(wsUUID, database.UUIDArray(ids)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "has_identity", "blocked", "opted_out", "has_consent"}).
			AddRow(known, true, false, false, true).
			AddRow(blocked, true, true, false, false).
			AddRow(quiet, false, false, true, false))

	got, err := NewSendFacts(db).LeadFacts(context.Background(), wsUUID, ids)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]campaign.LeadFacts{
		known:   {Found: true, HasIdentity: true, HasConsent: true},
		blocked: {Found: true, HasIdentity: true, Blocked: true},
		quiet:   {Found: true, OptedOut: true},
	}
	if len(got) != len(want) {
		t.Fatalf("LeadFacts() = %+v, want %+v", got, want)
	}
	for id, facts := range want {
		if got[id] != facts {
			t.Fatalf("LeadFacts()[%s] = %+v, want %+v", id, got[id], facts)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLeadFactsBindsOneArrayWhateverTheSize(t *testing.T) {
	if got := countPlaceholders(leadFactsSQL); got != 2 {
		t.Fatalf("leadFactsSQL has %d placeholders, want 2 (workspace and one id array)", got)
	}
	for _, want := range []string{"workspace_id = ?", "id = ANY(?::uuid[])", "deleted_at IS NULL"} {
		if !strings.Contains(leadFactsSQL, want) {
			t.Fatalf("leadFactsSQL lacks %q", want)
		}
	}
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	ids := make([]string, 70000)
	for i := range ids {
		ids[i] = fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
	}
	mock.ExpectQuery(exact(leadFactsSQL)).WithArgs(wsUUID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "has_identity", "blocked", "opted_out", "has_consent"}))
	if _, err := NewSendFacts(db).LeadFacts(context.Background(), wsUUID, ids); err != nil {
		t.Fatalf("70,000 ids must bind as one array: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLeadFactsRefusesWithoutAWorkspaceAndQueriesNothingWithoutIDs(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	facts := NewSendFacts(db)
	if _, err := facts.LeadFacts(context.Background(), " ", []string{uuid.NewString()}); err == nil {
		t.Fatal("a missing workspace must refuse")
	}
	got, err := facts.LeadFacts(context.Background(), wsUUID, []string{"nope", ""})
	if err != nil || len(got) != 0 {
		t.Fatalf("no valid id: (%v, %v)", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
