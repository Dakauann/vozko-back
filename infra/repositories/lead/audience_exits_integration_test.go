package lead

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/campaign"
	"vozko/infra/repositories/repotest"
)

func TestAudienceExitFactsAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_audience_exits")
	migrateLeadTables(t, db)
	ws, other := uuid.NewString(), uuid.NewString()
	now := time.Now().UTC()
	rows := []struct {
		workspace, number string
		blocked           bool
		optedOut, deleted *time.Time
	}{
		{workspace: ws, number: "5511987650001"},
		{workspace: ws, number: "551187650002", optedOut: &now},
		{workspace: ws, number: "5511987650003", blocked: true},
		{workspace: ws, number: "5511987650004", blocked: true, optedOut: &now},
		{workspace: ws, number: "5511987650005", optedOut: &now, deleted: &now},
		{workspace: other, number: "5511987650006", optedOut: &now},
		{workspace: ws, number: "5511987650007", optedOut: &now},
	}
	for i, r := range rows {
		if err := db.Exec(`INSERT INTO leads (id, workspace_id, number, name, blocked, opted_out_at, deleted_at, version, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1, now(), now())`,
			uuid.NewString(), r.workspace, r.number, "lead", r.blocked, r.optedOut, r.deleted).Error; err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	numbers := []string{"5511987650001", "5511987650002", "5511987650003", "5511987650004", "5511987650005", "5511987650006", "5511999999999"}
	groups, err := NewSendFacts(db).AudienceExitFacts(context.Background(), ws, numbers)
	if err != nil {
		t.Fatal(err)
	}
	got := campaign.TallyAudienceExits(groups)
	if got.OptedOut != 1 || got.Blocked != 2 {
		t.Fatalf("exits = %+v; want the ninth digit variant opted out and two blocked, never the deleted, the foreign or the lead outside the list", got)
	}
	total := 0
	for _, g := range groups {
		total += g.Leads
	}
	if total != 4 {
		t.Fatalf("matched %d leads, want 4", total)
	}
}
