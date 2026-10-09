package lead

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/campaign"
	"vozko/infra/repositories/repotest"
)

func TestLeadFactsAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_send_facts")
	migrateLeadTables(t, db)
	ws, other := uuid.NewString(), uuid.NewString()
	ids := map[string]string{}
	for _, name := range []string{"ok", "blocked", "quiet", "relative", "deleted", "foreign"} {
		ids[name] = uuid.NewString()
	}
	now := time.Now().UTC()
	rows := []struct {
		name, workspace, number   string
		blocked                   bool
		optedOut, consent, delete *time.Time
	}{
		{name: "ok", workspace: ws, number: "5511987650001", consent: &now},
		{name: "blocked", workspace: ws, number: "5511987650002", blocked: true},
		{name: "quiet", workspace: ws, number: "5511987650003", optedOut: &now},
		{name: "relative", workspace: ws},
		{name: "deleted", workspace: ws, number: "5511987650004", delete: &now},
		{name: "foreign", workspace: other, number: "5511987650005", consent: &now},
	}
	for _, r := range rows {
		var number any
		if r.number != "" {
			number = r.number
		}
		if err := db.Exec(`INSERT INTO leads (id, workspace_id, number, name, blocked, opted_out_at, whatsapp_opt_in_at, deleted_at, version, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, now(), now())`,
			ids[r.name], r.workspace, number, r.name, r.blocked, r.optedOut, r.consent, r.delete).Error; err != nil {
			t.Fatalf("insert %s: %v", r.name, err)
		}
	}

	all := []string{ids["ok"], ids["blocked"], ids["quiet"], ids["relative"], ids["deleted"], ids["foreign"], uuid.NewString()}
	got, err := NewSendFacts(db).LeadFacts(context.Background(), ws, all)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]campaign.LeadFacts{
		ids["ok"]:       {Found: true, HasIdentity: true, HasConsent: true},
		ids["blocked"]:  {Found: true, HasIdentity: true, Blocked: true},
		ids["quiet"]:    {Found: true, HasIdentity: true, OptedOut: true},
		ids["relative"]: {Found: true},
	}
	if len(got) != len(want) {
		t.Fatalf("LeadFacts() returned %d leads, want %d (deleted and foreign leads are absent): %+v", len(got), len(want), got)
	}
	for id, facts := range want {
		if got[id] != facts {
			t.Fatalf("LeadFacts()[%s] = %+v, want %+v", id, got[id], facts)
		}
	}
}
