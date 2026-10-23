package lead

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"vozko/domain/campaign"
)

func TestScreeningFactsReadsAChunkInOneStatementThatBindsTheIDsOnce(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	sender, ready, recent := uuid.NewString(), uuid.NewString(), uuid.NewString()
	sentAt := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)^WITH chunk AS \(SELECT unnest\(\$1::uuid\[\]\) AS lead_id\) SELECT .* FROM leads .*WHERE workspace_id = \$\d+ AND id IN \(SELECT lead_id FROM chunk\) AND deleted_at IS NULL$`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "has_identity", "blocked", "opted_out", "has_consent", "last_sent_at", "in_running_campaign"}).
			AddRow(ready, true, false, false, true, nil, false).
			AddRow(recent, true, false, false, false, sentAt, true))

	got, err := NewSendFacts(db).ScreeningFacts(context.Background(), campaign.ScreeningQuery{WorkspaceID: wsUUID, LeadIDs: []string{ready, recent, "not-a-uuid"}, SenderID: sender, Running: true})
	if err != nil {
		t.Fatal(err)
	}
	if f := got[ready]; f.Lead != (campaign.LeadFacts{Found: true, HasIdentity: true, HasConsent: true}) || f.LastSentAt != nil || f.InRunningCampaign {
		t.Fatalf("ready = %+v", f)
	}
	if f := got[recent]; f.LastSentAt == nil || !f.LastSentAt.Equal(sentAt) || !f.InRunningCampaign {
		t.Fatalf("recent = %+v", f)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestScreeningFactsAsksOnlyForTheFactsTheQueryNeeds(t *testing.T) {
	plain, plainArgs := screeningSQL(campaign.ScreeningQuery{WorkspaceID: wsUUID})
	if regexp.MustCompile(`lead_campaign_sends|campaign_entries`).MatchString(plain) || len(plainArgs) != 0 {
		t.Fatalf("without a sender or the running check the statement reads only the leads:\n%s %v", plain, plainArgs)
	}
	full, fullArgs := screeningSQL(campaign.ScreeningQuery{WorkspaceID: wsUUID, SenderID: uuid.NewString(), Running: true})
	for _, table := range []string{"lead_campaign_sends", "whatsapp_campaign_entries", "unofficial_whatsapp_campaign_entries"} {
		if !regexp.MustCompile(`\b` + table + `\b`).MatchString(full) {
			t.Fatalf("the full statement lacks %s:\n%s", table, full)
		}
	}
	if got := countPlaceholders(full); got != len(fullArgs)+2 {
		t.Fatalf("%d placeholders for %d args plus the workspace and the id array", got, len(fullArgs))
	}
	if strings.Count(full, "::uuid[]") != 1 {
		t.Fatalf("the chunk ids must be bound once: %s", full)
	}
}

func TestScreeningFactsRefusesWithoutAWorkspaceAndQueriesNothingWithoutIDs(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	facts := NewSendFacts(db)
	if _, err := facts.ScreeningFacts(context.Background(), campaign.ScreeningQuery{WorkspaceID: " ", LeadIDs: []string{uuid.NewString()}}); err == nil {
		t.Fatal("a missing workspace must refuse")
	}
	got, err := facts.ScreeningFacts(context.Background(), campaign.ScreeningQuery{WorkspaceID: wsUUID, LeadIDs: []string{"nope"}})
	if err != nil || len(got) != 0 {
		t.Fatalf("no valid id: (%v, %v)", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
