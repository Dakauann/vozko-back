package lead

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/campaign"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestScreeningFactsAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_screening_facts")
	migrateLeadTables(t, db)
	if err := db.AutoMigrate(&schema.LeadCampaignSend{}, &schema.WhatsAppCampaign{}, &schema.UnofficialWhatsAppCampaign{}, &schema.UnofficialWhatsAppCampaignEntry{}); err != nil {
		t.Fatal(err)
	}
	ws, other, sender, otherSender := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	ids := map[string]string{}
	for _, name := range []string{"ok", "recent", "elsewhere", "official", "unofficial", "finished", "foreign-run", "foreign"} {
		ids[name] = uuid.NewString()
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	for name, id := range ids {
		workspace := ws
		if name == "foreign" {
			workspace = other
		}
		exec(`INSERT INTO leads (id, workspace_id, number, name, version, created_at, updated_at) VALUES (?, ?, ?, ?, 1, now(), now())`, id, workspace, "55119"+id[:8], name)
	}
	recentAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	exec(`INSERT INTO lead_campaign_sends (id, lead_id, business_phone_id, campaign_id, sent_at) VALUES (?, ?, ?, ?, ?), (?, ?, ?, ?, ?), (?, ?, ?, ?, ?)`,
		uuid.NewString(), ids["recent"], sender, uuid.NewString(), recentAt.Add(-48*time.Hour),
		uuid.NewString(), ids["recent"], sender, uuid.NewString(), recentAt,
		uuid.NewString(), ids["elsewhere"], otherSender, uuid.NewString(), recentAt)
	running, stopped, foreignRunning := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO whatsapp_campaigns (id, workspace_id, name, status, created_at, updated_at) VALUES (?, ?, 'a', 'RUNNING', now(), now()), (?, ?, 'b', 'STOPPED', now(), now()), (?, ?, 'c', 'RUNNING', now(), now())`,
		running, ws, stopped, ws, foreignRunning, other)
	exec(`INSERT INTO whatsapp_campaign_entries (id, campaign_id, lead_id, status, created_at, updated_at) VALUES (?, ?, ?, 'PENDING', now(), now()), (?, ?, ?, 'SENT', now(), now()), (?, ?, ?, 'PENDING', now(), now())`,
		uuid.NewString(), running, ids["official"], uuid.NewString(), running, ids["finished"], uuid.NewString(), foreignRunning, ids["foreign-run"])
	unofficial := uuid.NewString()
	exec(`INSERT INTO unofficial_whatsapp_campaigns (id, workspace_id, name, status, created_at, updated_at) VALUES (?, ?, 'u', 'RUNNING', now(), now())`, unofficial, ws)
	exec(`INSERT INTO unofficial_whatsapp_campaign_entries (id, workspace_id, campaign_id, lead_id, number, status, created_at, updated_at) VALUES (?, ?, ?, ?, '5511900000000', 'PENDING', now(), now())`,
		uuid.NewString(), ws, unofficial, ids["unofficial"])

	all := []string{ids["ok"], ids["recent"], ids["elsewhere"], ids["official"], ids["unofficial"], ids["finished"], ids["foreign-run"], ids["foreign"]}
	got, err := NewSendFacts(db).ScreeningFacts(context.Background(), campaign.ScreeningQuery{WorkspaceID: ws, LeadIDs: all, SenderID: sender, Running: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 7 {
		t.Fatalf("got %d leads, want the 7 of the workspace: %+v", len(got), got)
	}
	if f := got[ids["recent"]]; f.LastSentAt == nil || !f.LastSentAt.Equal(recentAt) {
		t.Fatalf("recent = %+v, want the latest send from this sender", f)
	}
	if f := got[ids["elsewhere"]]; f.LastSentAt != nil {
		t.Fatalf("a send from another number = %+v", f)
	}
	for name, want := range map[string]bool{"ok": false, "official": true, "unofficial": true, "finished": false, "foreign-run": false} {
		if f := got[ids[name]]; f.InRunningCampaign != want || !f.Lead.Found || !f.Lead.HasIdentity {
			t.Fatalf("%s = %+v, want running %v", name, f, want)
		}
	}
	quiet, err := NewSendFacts(db).ScreeningFacts(context.Background(), campaign.ScreeningQuery{WorkspaceID: ws, LeadIDs: all})
	if err != nil {
		t.Fatal(err)
	}
	if f := quiet[ids["official"]]; f.InRunningCampaign || quiet[ids["recent"]].LastSentAt != nil {
		t.Fatalf("without a sender or the running check = %+v", quiet)
	}
}
