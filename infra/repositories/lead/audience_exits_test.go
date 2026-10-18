package lead

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"vozko/domain/campaign"
)

func TestAudienceExitFactsGroupTheMatchedLeadsInOneStatement(t *testing.T) {
	if got := countPlaceholders(audienceExitFactsSQL); got != 2 {
		t.Fatalf("audienceExitFactsSQL has %d placeholders, want 2 (workspace and one number array)", got)
	}
	for _, want := range []string{"workspace_id = ?", "number = ANY(?)", "deleted_at IS NULL", "GROUP BY"} {
		if !strings.Contains(audienceExitFactsSQL, want) {
			t.Fatalf("audienceExitFactsSQL lacks %q", want)
		}
	}
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(exact(audienceExitFactsSQL)).
		WithArgs(wsUUID, pq.StringArray{"5511987650001", "551187650001"}).
		WillReturnRows(sqlmock.NewRows([]string{"blocked", "opted_out", "leads"}).
			AddRow(false, false, 40).
			AddRow(false, true, 3).
			AddRow(true, false, 1))

	got, err := NewSendFacts(db).AudienceExitFacts(context.Background(), wsUUID, []string{"5511987650001"})
	if err != nil {
		t.Fatal(err)
	}
	want := []campaign.LeadFactsGroup{
		{Facts: campaign.LeadFacts{Found: true, HasIdentity: true}, Leads: 40},
		{Facts: campaign.LeadFacts{Found: true, HasIdentity: true, OptedOut: true}, Leads: 3},
		{Facts: campaign.LeadFacts{Found: true, HasIdentity: true, Blocked: true}, Leads: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("groups = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("group %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAudienceExitFactsRefuseWithoutAWorkspaceAndQueryNothingWithoutNumbers(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	facts := NewSendFacts(db)
	if _, err := facts.AudienceExitFacts(context.Background(), " ", []string{"5511987650001"}); err == nil {
		t.Fatal("a missing workspace must refuse")
	}
	got, err := facts.AudienceExitFacts(context.Background(), wsUUID, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("no numbers = %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
