package whatsapp_repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"vozko/domain/whatsapp/messagestatus"
	"vozko/infra/database/schema"
)

func messageStatusDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := qualityIntegrationDB(t)
	for _, ddl := range []string{
		`CREATE TABLE whatsapp_templates (id uuid PRIMARY KEY, external_id varchar(100) NOT NULL, waba_id varchar(36) NOT NULL DEFAULT '')`,
		`CREATE TABLE whatsapp_campaigns (id uuid PRIMARY KEY, template_id uuid)`,
		`CREATE TABLE whatsapp_campaign_entries (id uuid PRIMARY KEY, campaign_id uuid NOT NULL, message_id varchar(100))`,
		`CREATE INDEX ON whatsapp_campaign_entries (message_id)`,
		`CREATE TABLE whatsapp_template_sends (id uuid PRIMARY KEY, template_id uuid NOT NULL, provider_message_id varchar(191))`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("ddl: %v", err)
		}
	}
	if err := db.AutoMigrate(&schema.WhatsAppMessageStatusEvent{}, &schema.WhatsAppTemplateHourlyStat{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedCampaignMessage(t *testing.T, db *gorm.DB, templateID, externalID, wabaID, messageID string) {
	t.Helper()
	stmts := []struct {
		sql  string
		args []interface{}
	}{
		{`INSERT INTO whatsapp_templates (id, external_id, waba_id) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`, []interface{}{templateID, externalID, wabaID}},
		{`INSERT INTO whatsapp_campaigns (id, template_id) VALUES (gen_random_uuid(), ?)`, []interface{}{templateID}},
		{`INSERT INTO whatsapp_campaign_entries (id, campaign_id, message_id) SELECT gen_random_uuid(), id, ? FROM whatsapp_campaigns WHERE template_id = ? LIMIT 1`, []interface{}{messageID, templateID}},
	}
	for _, s := range stmts {
		if err := db.Exec(s.sql, s.args...).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

func statusAt(messageID string, status messagestatus.Status, at time.Time, code int) messagestatus.Event {
	return messagestatus.Event{MessageID: messageID, PhoneNumberID: "phone-1", Status: status, ErrorCode: code, OccurredAt: at}
}

type hourlyRow struct {
	TemplateExternalID string
	Hour               time.Time
	Status             string
	ErrorCode          int
	Messages           int64
}

func hourly(t *testing.T, db *gorm.DB) map[string]int64 {
	t.Helper()
	var rows []hourlyRow
	if err := db.Raw(`SELECT template_external_id, hour, status, error_code, messages FROM whatsapp_template_hourly_stats`).Scan(&rows).Error; err != nil {
		t.Fatalf("read hourly: %v", err)
	}
	out := map[string]int64{}
	for _, r := range rows {
		out[fmt.Sprintf("%s|%s|%s|%d", r.TemplateExternalID, r.Hour.UTC().Format("15"), r.Status, r.ErrorCode)] = r.Messages
	}
	return out
}

func TestMessageStatusIntegration_ARedeliveredWebhookIsStoredOnce(t *testing.T) {
	db := messageStatusDB(t)
	repo := NewMessageStatusRepository(db)
	at := time.Date(2026, 10, 15, 14, 5, 0, 0, time.UTC)

	for i := 0; i < 2; i++ {
		if err := repo.Record(statusAt("wamid.A", messagestatus.StatusRead, at, 0)); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	var count int64
	db.Model(&schema.WhatsAppMessageStatusEvent{}).Count(&count)
	if count != 1 {
		t.Fatalf("want 1 row, got %d", count)
	}
}

func TestMessageStatusIntegration_AttributesCampaignAndDirectSendsAndSetsAsideTheRest(t *testing.T) {
	db := messageStatusDB(t)
	repo := NewMessageStatusRepository(db)
	ctx := context.Background()
	at := time.Date(2026, 10, 15, 14, 5, 0, 0, time.UTC)
	seedCampaignMessage(t, db, "00000000-0000-0000-0000-000000000001", "tpl-campaign", "waba-1", "wamid.campaign")
	if err := db.Exec(`INSERT INTO whatsapp_templates (id, external_id, waba_id) VALUES ('00000000-0000-0000-0000-000000000002', 'tpl-crm', 'waba-2')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO whatsapp_template_sends (id, template_id, provider_message_id) VALUES (gen_random_uuid(), '00000000-0000-0000-0000-000000000002', 'wamid.crm')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.Record(
		statusAt("wamid.campaign", messagestatus.StatusSent, at, 0),
		statusAt("wamid.crm", messagestatus.StatusDelivered, at, 0),
		statusAt("wamid.service", messagestatus.StatusRead, at, 0),
	); err != nil {
		t.Fatal(err)
	}

	attributed, err := repo.AttributePending(ctx, at.Add(-time.Hour))
	if err != nil || attributed != 2 {
		t.Fatalf("attributed %d, %v", attributed, err)
	}
	var pending int64
	db.Model(&schema.WhatsAppMessageStatusEvent{}).Where("attributed_at IS NULL").Count(&pending)
	if pending != 1 {
		t.Fatalf("the service message waits for its chance, pending %d", pending)
	}

	if _, err := repo.AttributePending(ctx, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var setAside schema.WhatsAppMessageStatusEvent
	db.Where("message_id = ?", "wamid.service").First(&setAside)
	if setAside.AttributedAt == nil || setAside.RolledUpAt == nil || setAside.TemplateExternalID != "" {
		t.Fatalf("after the wait it is set aside as not a template: %+v", setAside)
	}
}

func TestMessageStatusIntegration_UnmatchedEventsNeverStarveANewOne(t *testing.T) {
	db := messageStatusDB(t)
	repo := NewMessageStatusRepository(db)
	at := time.Date(2026, 10, 15, 14, 5, 0, 0, time.UTC)
	if err := db.Exec(`INSERT INTO whatsapp_message_status_events (message_id, status, phone_number_id, occurred_at, recorded_at)
		SELECT 'wamid.noise.' || g, 'read', 'phone-1', ?, now() FROM generate_series(1, ?) g`, at, attributionPage+1).Error; err != nil {
		t.Fatal(err)
	}
	seedCampaignMessage(t, db, "00000000-0000-0000-0000-000000000003", "tpl-late", "waba-1", "wamid.real")
	if err := repo.Record(statusAt("wamid.real", messagestatus.StatusSent, at, 0)); err != nil {
		t.Fatal(err)
	}

	attributed, err := repo.AttributePending(context.Background(), at.Add(-time.Hour))
	if err != nil || attributed != 1 {
		t.Fatalf("the real send behind %d unmatched events is attributed, got %d, %v", attributionPage+1, attributed, err)
	}
}

func TestMessageStatusIntegration_RollUpCountsByHourStatusAndCodeAndRecountsLateEventsOnce(t *testing.T) {
	db := messageStatusDB(t)
	repo := NewMessageStatusRepository(db)
	ctx := context.Background()
	h14 := time.Date(2026, 10, 15, 14, 0, 0, 0, time.UTC)
	for i, id := range []string{"wamid.1", "wamid.2", "wamid.3"} {
		seedCampaignMessage(t, db, "00000000-0000-0000-0000-000000000004", "tpl-wave", "waba-1", id)
		if err := repo.Record(statusAt(id, messagestatus.StatusSent, h14.Add(time.Duration(i)*time.Minute), 0)); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Record(statusAt("wamid.3", messagestatus.StatusFailed, h14.Add(10*time.Minute), 131026)); err != nil {
		t.Fatal(err)
	}
	recountAfter := h14.Add(-24 * time.Hour)
	if _, err := repo.AttributePending(ctx, h14.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RollUpPending(ctx, recountAfter); err != nil {
		t.Fatal(err)
	}
	got := hourly(t, db)
	if got["tpl-wave|14|sent|0"] != 3 || got["tpl-wave|14|failed|131026"] != 1 {
		t.Fatalf("hourly %v", got)
	}

	if err := repo.Record(statusAt("wamid.1", messagestatus.StatusRead, h14.Add(50*time.Minute), 0), statusAt("wamid.2", messagestatus.StatusRead, h14.Add(70*time.Minute), 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AttributePending(ctx, h14.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RollUpPending(ctx, recountAfter); err != nil {
		t.Fatal(err)
	}
	got = hourly(t, db)
	if got["tpl-wave|14|sent|0"] != 3 || got["tpl-wave|14|read|0"] != 1 || got["tpl-wave|15|read|0"] != 1 {
		t.Fatalf("a late read recounts its hour without doubling the sends: %v", got)
	}
	var unrolled int64
	db.Model(&schema.WhatsAppMessageStatusEvent{}).Where("rolled_up_at IS NULL").Count(&unrolled)
	if unrolled != 0 {
		t.Fatalf("everything attributed is rolled up, %d left", unrolled)
	}
}

func TestMessageStatusIntegration_AnHourBeyondTheRecountHorizonIsNeverOverwritten(t *testing.T) {
	db := messageStatusDB(t)
	repo := NewMessageStatusRepository(db)
	ctx := context.Background()
	old := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if err := db.Create(&schema.WhatsAppTemplateHourlyStat{TemplateExternalID: "tpl-old", PhoneNumberID: "phone-1", Hour: old, Status: "sent", ErrorCode: 0, Messages: 900}).Error; err != nil {
		t.Fatal(err)
	}
	seedCampaignMessage(t, db, "00000000-0000-0000-0000-000000000005", "tpl-old", "waba-1", "wamid.ancient")
	if err := repo.Record(statusAt("wamid.ancient", messagestatus.StatusSent, old.Add(5*time.Minute), 0)); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.AttributePending(ctx, old); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RollUpPending(ctx, old.Add(30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	if got := hourly(t, db)["tpl-old|10|sent|0"]; got != 900 {
		t.Fatalf("the old hour keeps its count, got %d", got)
	}
}

func TestMessageStatusIntegration_PurgeOnlyRemovesOldRolledUpEvents(t *testing.T) {
	db := messageStatusDB(t)
	repo := NewMessageStatusRepository(db)
	ctx := context.Background()
	cutoff := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	rolled := cutoff.Add(-time.Hour)
	if err := db.Exec(`INSERT INTO whatsapp_message_status_events (message_id, status, phone_number_id, occurred_at, recorded_at, attributed_at, rolled_up_at) VALUES
		('wamid.old-rolled', 'read', 'phone-1', ?, now(), now(), now()),
		('wamid.old-unrolled', 'read', 'phone-1', ?, now(), now(), NULL),
		('wamid.new-rolled', 'read', 'phone-1', ?, now(), now(), now())`, rolled, rolled, cutoff.Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}

	purged, err := repo.PurgeRolledUpBefore(ctx, cutoff)

	if err != nil || purged != 1 {
		t.Fatalf("purged %d, %v", purged, err)
	}
	var left []string
	db.Model(&schema.WhatsAppMessageStatusEvent{}).Order("message_id").Pluck("message_id", &left)
	if len(left) != 2 || left[0] != "wamid.new-rolled" || left[1] != "wamid.old-unrolled" {
		t.Fatalf("nothing not yet counted is ever deleted, left %v", left)
	}
}
