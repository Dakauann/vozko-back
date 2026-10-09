package leadsend_repository

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
	wc_repository "vozko/infra/repositories/whatsapp_campaign"
)

func sendDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "leadsend")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.Lead{}, &schema.LeadMessageWindow{}, &schema.WhatsAppCampaign{}, &schema.WhatsAppCampaignEntry{},
		&schema.UnofficialWhatsAppCampaign{}, &schema.UnofficialWhatsAppCampaignEntry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, name := range []string{"ux_wa_campaign_idem", "ux_uwc_campaign_idem"} {
		sql, ok := database.PerformanceIndexSQL(name)
		if !ok {
			t.Fatalf("index %s is not declared", name)
		}
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("index %s: %v", name, err)
		}
	}
	return db
}

func seedLead(t *testing.T, db *gorm.DB, workspaceID string, consent bool) string {
	t.Helper()
	l := schema.Lead{ID: uuid.NewString(), WorkspaceID: workspaceID, Number: schema.OptionalText("55119" + uuid.NewString()[:8]), Name: "Maria"}
	if consent {
		at := time.Now().UTC()
		l.WhatsAppOptInAt = &at
	}
	if err := db.Create(&l).Error; err != nil {
		t.Fatalf("lead: %v", err)
	}
	return l.ID
}

func seedCampaign(t *testing.T, db *gorm.DB, workspaceID, status string) string {
	t.Helper()
	c := schema.WhatsAppCampaign{ID: uuid.NewString(), WorkspaceID: workspaceID, Name: "Matrículas", Status: status}
	if err := db.Create(&c).Error; err != nil {
		t.Fatalf("campaign: %v", err)
	}
	return c.ID
}

func seedEntry(t *testing.T, db *gorm.DB, campaignID, leadID, status string, code int, at time.Time) {
	t.Helper()
	e := schema.WhatsAppCampaignEntry{ID: uuid.NewString(), CampaignID: campaignID, LeadID: leadID, Status: status, ErrorCode: code, CreatedAt: at}
	if err := db.Create(&e).Error; err != nil {
		t.Fatalf("entry: %v", err)
	}
}

func TestTallyCountsAFrozenSendAgainstPostgres(t *testing.T) {
	db := sendDB(t)
	ws, phone := uuid.NewString(), uuid.NewString()
	id := seedCampaign(t, db, ws, "STOPPED")
	now := time.Now().UTC()
	consented, windowed, plain := seedLead(t, db, ws, true), seedLead(t, db, ws, false), seedLead(t, db, ws, false)
	blocked, missing := seedLead(t, db, ws, false), seedLead(t, db, ws, false)
	seedEntry(t, db, id, consented, "PENDING", 0, now)
	seedEntry(t, db, id, windowed, "PENDING", 0, now)
	seedEntry(t, db, id, plain, "PENDING", 0, now)
	seedEntry(t, db, id, blocked, "FAILED", campaign.SkipBlocked.FailureCode(), now)
	seedEntry(t, db, id, missing, "FAILED", campaign.SkipMissingVariable.FailureCode(), now)
	if err := db.Exec(`UPDATE whatsapp_campaign_entries SET error_message = ? WHERE campaign_id = ? AND lead_id = ?`, "missing_variable:2:lead.district", id, missing).Error; err != nil {
		t.Fatalf("missing slot: %v", err)
	}
	legacy := seedLead(t, db, ws, false)
	seedEntry(t, db, id, legacy, "FAILED", campaign.SkipMissingVariable.FailureCode(), now)
	if err := db.Exec(`UPDATE whatsapp_campaign_entries SET error_message = NULL WHERE campaign_id = ? AND lead_id = ?`, id, legacy).Error; err != nil {
		t.Fatalf("legacy entry: %v", err)
	}
	both := seedLead(t, db, ws, false)
	seedEntry(t, db, id, both, "FAILED", campaign.SkipMissingVariable.FailureCode(), now)
	if err := db.Exec(`UPDATE whatsapp_campaign_entries SET error_message = ? WHERE campaign_id = ? AND lead_id = ?`, "missing_variable:1:lead.name;2:lead.district", id, both).Error; err != nil {
		t.Fatalf("two slots: %v", err)
	}
	for _, message := range []string{"cooldown:7", "cooldown:30", "cooldown"} {
		recent := seedLead(t, db, ws, false)
		seedEntry(t, db, id, recent, string(campaign.SendStatusNotEligiblePossibleSpam), campaign.SkipCooldown.FailureCode(), now)
		if err := db.Exec(`UPDATE whatsapp_campaign_entries SET error_message = ? WHERE campaign_id = ? AND lead_id = ?`, message, id, recent).Error; err != nil {
			t.Fatalf("cooldown entry: %v", err)
		}
	}
	if err := db.Create(&schema.LeadMessageWindow{ID: uuid.NewString(), LeadID: windowed, BusinessPhoneID: phone, LastMessageAt: now.Add(-time.Hour)}).Error; err != nil {
		t.Fatalf("window: %v", err)
	}

	parts, err := NewStore(db).Tally(context.Background(), campaign.ChannelOfficial, ws, []string{id}, phone, now)
	if err != nil {
		t.Fatalf("Tally: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("parts = %+v", parts)
	}
	p := parts[0]
	if p.Entries != 10 || p.Eligible != 3 || p.Skipped[campaign.SkipBlocked] != 1 || p.Skipped[campaign.SkipMissingVariable] != 3 || p.Skipped[campaign.SkipCooldown] != 3 {
		t.Fatalf("tally = %+v", p)
	}
	wantMissing := map[campaign.MissingVariable]int{{Slot: 2, Source: campaign.BindDistrict}: 2, {Slot: 1, Source: campaign.BindName}: 1}
	if !reflect.DeepEqual(p.Missing, wantMissing) {
		t.Fatalf("missing = %+v, want %+v (every named slot counts, the legacy entry only in skipped)", p.Missing, wantMissing)
	}
	if p.CooldownDays != 30 {
		t.Fatalf("cooldown days = %d, want the longest the entries recorded", p.CooldownDays)
	}
	if p.Counted[campaign.CountedWindowOpen] != 1 || p.Counted[campaign.CountedNoConsentRecorded] != 2 {
		t.Fatalf("counted = %+v", p.Counted)
	}
	other, err := NewStore(db).Tally(context.Background(), campaign.ChannelOfficial, uuid.NewString(), []string{id}, phone, now)
	if err != nil || len(other) != 0 {
		t.Fatalf("another workspace read the send: %+v, %v", other, err)
	}
}

func TestInRunningCampaignsAgainstPostgres(t *testing.T) {
	db := sendDB(t)
	ws := uuid.NewString()
	running, stopped := seedCampaign(t, db, ws, "RUNNING"), seedCampaign(t, db, ws, "STOPPED")
	busy, idle, done := seedLead(t, db, ws, false), seedLead(t, db, ws, false), seedLead(t, db, ws, false)
	seedEntry(t, db, running, busy, "PENDING", 0, time.Now())
	seedEntry(t, db, running, done, "SENT", 0, time.Now())
	seedEntry(t, db, stopped, idle, "PENDING", 0, time.Now())
	unofficialRunning := schema.UnofficialWhatsAppCampaign{ID: uuid.NewString(), WorkspaceID: ws, InstanceID: uuid.NewString(), Name: "x", Status: "RUNNING"}
	if err := db.Create(&unofficialRunning).Error; err != nil {
		t.Fatalf("unofficial campaign: %v", err)
	}
	other := seedLead(t, db, ws, false)
	if err := db.Create(&schema.UnofficialWhatsAppCampaignEntry{ID: uuid.NewString(), CampaignID: unofficialRunning.ID, WorkspaceID: ws, LeadID: other, Number: "5511", Status: "PENDING"}).Error; err != nil {
		t.Fatalf("unofficial entry: %v", err)
	}

	got, err := NewStore(db).InRunningCampaigns(context.Background(), ws, []string{busy, idle, done, other})
	if err != nil {
		t.Fatalf("InRunningCampaigns: %v", err)
	}
	if !got[busy] || !got[other] || got[idle] || got[done] || len(got) != 2 {
		t.Fatalf("running = %v", got)
	}
}

func TestSkipBeyondKeepsTheFirstEntriesInLeadOrderAgainstPostgres(t *testing.T) {
	db := sendDB(t)
	ws := uuid.NewString()
	id := seedCampaign(t, db, ws, "STOPPED")
	base := time.Now().UTC().Add(-time.Hour)
	leads := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		l := seedLead(t, db, ws, false)
		leads = append(leads, l)
		seedEntry(t, db, id, l, "PENDING", 0, base)
	}
	sort.Strings(leads)
	n, err := NewStore(db).SkipBeyond(context.Background(), campaign.ChannelOfficial, ws, id, 2)
	if err != nil || n != 3 {
		t.Fatalf("SkipBeyond = %d, %v", n, err)
	}
	var kept []string
	if err := db.Raw(`SELECT lead_id FROM whatsapp_campaign_entries WHERE campaign_id = ? AND status = 'PENDING' ORDER BY lead_id`, id).Scan(&kept).Error; err != nil {
		t.Fatal(err)
	}
	if len(kept) != 2 || kept[0] != leads[0] || kept[1] != leads[1] {
		t.Fatalf("kept = %v, want the two lowest lead ids of %v", kept, leads)
	}
	var overCap int64
	db.Raw(`SELECT COUNT(*) FROM whatsapp_campaign_entries WHERE campaign_id = ? AND error_code = ?`, id, campaign.SkipOverCap.FailureCode()).Scan(&overCap)
	if overCap != 3 {
		t.Fatalf("over cap = %d", overCap)
	}
	running := seedCampaign(t, db, ws, "RUNNING")
	seedEntry(t, db, running, seedLead(t, db, ws, false), "PENDING", 0, base)
	if n, err := NewStore(db).SkipBeyond(context.Background(), campaign.ChannelOfficial, ws, running, 0); err != nil || n != 0 {
		t.Fatalf("a running campaign was trimmed: %d, %v", n, err)
	}
}

func TestSkipInRunningMarksOnlyTheLeadsPendingElsewhereAgainstPostgres(t *testing.T) {
	db := sendDB(t)
	ws := uuid.NewString()
	part, running, finished := seedCampaign(t, db, ws, "STOPPED"), seedCampaign(t, db, ws, "RUNNING"), seedCampaign(t, db, ws, "COMPLETED")
	busy, sent, free := seedLead(t, db, ws, false), seedLead(t, db, ws, false), seedLead(t, db, ws, false)
	for _, l := range []string{busy, sent, free} {
		seedEntry(t, db, part, l, "PENDING", 0, time.Now())
	}
	seedEntry(t, db, running, busy, "PENDING", 0, time.Now())
	seedEntry(t, db, running, sent, "SENT", 0, time.Now())
	seedEntry(t, db, finished, free, "PENDING", 0, time.Now())

	n, err := NewStore(db).SkipInRunning(context.Background(), campaign.ChannelOfficial, ws, part)
	if err != nil || n != 1 {
		t.Fatalf("SkipInRunning = %d, %v, want only the lead pending in the running campaign", n, err)
	}
	var code int
	db.Raw(`SELECT error_code FROM whatsapp_campaign_entries WHERE campaign_id = ? AND lead_id = ?`, part, busy).Scan(&code)
	if code != campaign.SkipAlreadyInRunningCampaign.FailureCode() {
		t.Fatalf("code = %d", code)
	}
	if n, err := NewStore(db).SkipInRunning(context.Background(), campaign.ChannelOfficial, ws, running); err != nil || n != 0 {
		t.Fatalf("a running part was trimmed: %d, %v", n, err)
	}
}

func TestDeleteStoppedRacesAStartAgainstPostgres(t *testing.T) {
	db := sendDB(t)
	ws := uuid.NewString()
	stopped, started := seedCampaign(t, db, ws, "STOPPED"), seedCampaign(t, db, ws, "RUNNING")
	for _, id := range []string{stopped, started} {
		seedEntry(t, db, id, seedLead(t, db, ws, false), "PENDING", 0, time.Now())
	}
	store := NewStore(db)
	if deleted, err := store.DeleteStopped(context.Background(), campaign.ChannelOfficial, ws, started); err != nil || deleted {
		t.Fatalf("a started campaign was deleted: %v, %v", deleted, err)
	}
	if deleted, err := store.DeleteStopped(context.Background(), campaign.ChannelOfficial, uuid.NewString(), stopped); err != nil || deleted {
		t.Fatalf("another workspace deleted the campaign: %v, %v", deleted, err)
	}
	if deleted, err := store.DeleteStopped(context.Background(), campaign.ChannelOfficial, ws, stopped); err != nil || !deleted {
		t.Fatalf("DeleteStopped = %v, %v", deleted, err)
	}
	var live int64
	db.Raw(`SELECT COUNT(*) FROM whatsapp_campaign_entries WHERE campaign_id = ? AND deleted_at IS NULL`, stopped).Scan(&live)
	if live != 0 {
		t.Fatalf("live entries = %d", live)
	}
	db.Raw(`SELECT COUNT(*) FROM whatsapp_campaign_entries WHERE campaign_id = ? AND deleted_at IS NULL`, started).Scan(&live)
	if live != 1 {
		t.Fatalf("the started campaign lost its entries: %d", live)
	}
}

func TestOneSendKeyHoldsOneLiveCampaignAgainstPostgres(t *testing.T) {
	db := sendDB(t)
	ws := uuid.NewString()
	repo := wc_repository.NewRepository(db)
	first := &wc.Campaign{ID: uuid.NewString(), WorkspaceID: ws, Name: "Matrículas (1/2)", Status: wc.CampaignStatusStopped, Source: campaign.SourceLeadSelection, IdempotencyKey: "base:1/2"}
	if err := repo.Create(first); err != nil {
		t.Fatalf("first: %v", err)
	}
	second := &wc.Campaign{ID: uuid.NewString(), WorkspaceID: ws, Name: "Matrículas (2/2)", Status: wc.CampaignStatusStopped, Source: campaign.SourceLeadSelection, IdempotencyKey: "base:2/2"}
	if err := repo.Create(second); err != nil {
		t.Fatalf("second: %v", err)
	}
	duplicate := &wc.Campaign{ID: uuid.NewString(), WorkspaceID: ws, Name: "again", Status: wc.CampaignStatusStopped, IdempotencyKey: "base:1/2"}
	if err := repo.Create(duplicate); !errors.Is(err, campaign.ErrIdempotencyKeyTaken) {
		t.Fatalf("a second campaign under one key = %v", err)
	}
	elsewhere := &wc.Campaign{ID: uuid.NewString(), WorkspaceID: uuid.NewString(), Name: "other workspace", Status: wc.CampaignStatusStopped, IdempotencyKey: "base:1/2"}
	if err := repo.Create(elsewhere); err != nil {
		t.Fatalf("another workspace cannot reuse a key: %v", err)
	}
	parts, err := NewStore(db).KeyedParts(context.Background(), campaign.ChannelOfficial, ws, "base")
	if err != nil || len(parts) != 2 {
		t.Fatalf("parts = %+v, %v", parts, err)
	}
	if ids, ok := campaign.CompleteParts("base", parts); !ok || ids[0] != first.ID || ids[1] != second.ID {
		t.Fatalf("CompleteParts = %v %v", ids, ok)
	}
	found, err := repo.(interface {
		FindByIdempotencyKey(string, string) (*wc.Campaign, error)
	}).FindByIdempotencyKey(ws, "base:2/2")
	if err != nil || found.ID != second.ID || found.Source != campaign.SourceLeadSelection {
		t.Fatalf("found = %+v, %v", found, err)
	}
	if err := repo.Delete(first.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := repo.Create(duplicate); err != nil {
		t.Fatalf("a cancelled send freed its key: %v", err)
	}
}

func TestAKeyedCampaignIsWrittenWithItsEntriesOrNotAtAllAgainstPostgres(t *testing.T) {
	db := sendDB(t)
	ws := uuid.NewString()
	repo := wc_repository.NewRepository(db).(interface {
		CreateWithEntries(*wc.Campaign, []wce.WhatsAppCampaignEntry) error
		FindByIdempotencyKey(string, string) (*wc.Campaign, error)
	})
	draft := func() *wc.Campaign {
		return &wc.Campaign{ID: uuid.NewString(), WorkspaceID: ws, Name: "Matrículas", Status: wc.CampaignStatusStopped, Source: campaign.SourceLeadSelection, IdempotencyKey: "atomic:1/1"}
	}
	broken := draft()
	if err := repo.CreateWithEntries(broken, []wce.WhatsAppCampaignEntry{{ID: uuid.NewString(), CampaignID: broken.ID, LeadID: "not-a-uuid", Status: wce.SendStatusPending}}); err == nil {
		t.Fatal("an entry insert that cannot succeed reported success")
	}
	if _, err := repo.FindByIdempotencyKey(ws, "atomic:1/1"); !errors.Is(err, wc.ErrCampaignNotFound) {
		t.Fatalf("the key names a campaign without its entries: %v", err)
	}
	whole := draft()
	leadA, leadB := seedLead(t, db, ws, false), seedLead(t, db, ws, false)
	entries := []wce.WhatsAppCampaignEntry{
		{ID: uuid.NewString(), CampaignID: whole.ID, LeadID: leadA, Status: wce.SendStatusPending},
		{ID: uuid.NewString(), CampaignID: whole.ID, LeadID: leadB, Status: wce.SendStatusPending},
	}
	if err := repo.CreateWithEntries(whole, entries); err != nil {
		t.Fatalf("CreateWithEntries: %v", err)
	}
	var count int64
	db.Raw(`SELECT COUNT(*) FROM whatsapp_campaign_entries WHERE campaign_id = ?`, whole.ID).Scan(&count)
	if count != 2 {
		t.Fatalf("entries = %d, want 2", count)
	}
	if err := repo.CreateWithEntries(draft(), entries[:1]); !errors.Is(err, campaign.ErrIdempotencyKeyTaken) {
		t.Fatalf("a second campaign under the key = %v, want ErrIdempotencyKeyTaken", err)
	}
}
