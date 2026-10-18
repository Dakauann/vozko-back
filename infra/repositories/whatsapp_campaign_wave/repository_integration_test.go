package whatsapp_campaign_wave_repository

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	sc "vozko/domain/whatsapp_campaign/smartcampaign"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

var at = time.Date(2026, 10, 16, 13, 0, 0, 0, time.UTC)

func waveDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "campaign_waves", &schema.WhatsAppCampaignWavePolicy{}, &schema.WhatsAppCampaignWaveState{}, &schema.WhatsAppCampaignWave{})
	for _, ddl := range []string{
		`CREATE TABLE whatsapp_campaigns (id uuid PRIMARY KEY, status varchar(20) NOT NULL, template_id uuid, business_phone_id uuid, deleted_at timestamptz)`,
		`CREATE TABLE leads (id uuid PRIMARY KEY, number varchar(20))`,
		`CREATE TABLE whatsapp_campaign_entries (id uuid PRIMARY KEY, campaign_id uuid NOT NULL, lead_id uuid NOT NULL, status varchar(40) NOT NULL DEFAULT 'PENDING',
			wave_number int NOT NULL DEFAULT 0, error_code int NOT NULL DEFAULT 0, created_at timestamptz NOT NULL, deleted_at timestamptz)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("ddl: %v", err)
		}
	}
	return db
}

func addCampaign(t *testing.T, db *gorm.DB, status string, contacts int) string {
	t.Helper()
	id := uuid.NewString()
	if err := db.Exec(`INSERT INTO whatsapp_campaigns (id, status, template_id, business_phone_id) VALUES (?, ?, ?, ?)`, id, status, uuid.NewString(), uuid.NewString()).Error; err != nil {
		t.Fatalf("campaign: %v", err)
	}
	for i := 0; i < contacts; i++ {
		lead := uuid.NewString()
		db.Exec(`INSERT INTO leads (id, number) VALUES (?, ?)`, lead, fmt.Sprintf("55849%08d", i))
		db.Exec(`INSERT INTO whatsapp_campaign_entries (id, campaign_id, lead_id, created_at) VALUES (?, ?, ?, ?)`, uuid.NewString(), id, lead, at.Add(time.Duration(i)*time.Second))
	}
	return id
}

func setEntries(t *testing.T, db *gorm.DB, campaignID string, number, count int, status string, code int) {
	t.Helper()
	err := db.Exec(`UPDATE whatsapp_campaign_entries SET status = ?, error_code = ? WHERE id IN (
		SELECT id FROM whatsapp_campaign_entries WHERE campaign_id = ? AND wave_number = ? AND status = 'PENDING' ORDER BY created_at LIMIT ?)`,
		status, code, campaignID, number, count).Error
	if err != nil {
		t.Fatalf("set entries: %v", err)
	}
}

func TestWaveIntegration_AClaimTakesTheOldestFreeContactsOnce(t *testing.T) {
	db := waveDB(t)
	repo := NewRepository(db)
	id := addCampaign(t, db, "RUNNING", 5)

	first, err := repo.ClaimWave(id, 1, 3, at)
	if err != nil || first.Size != 3 || first.State != sc.Released {
		t.Fatalf("first claim = %+v, %v", first, err)
	}
	if _, err := repo.ClaimWave(id, 1, 3, at); !errors.Is(err, sc.ErrAlreadyReleased) {
		t.Fatalf("claiming the same wave again = %v, want ErrAlreadyReleased", err)
	}
	second, err := repo.ClaimWave(id, 2, 10, at)
	if err != nil || second.Size != 2 {
		t.Fatalf("second claim = %+v, %v, want only the 2 left", second, err)
	}
	if _, err := repo.ClaimWave(id, 3, 10, at); !errors.Is(err, sc.ErrNothingToClaim) {
		t.Fatalf("claiming with nothing left = %v, want ErrNothingToClaim", err)
	}
	if waves, _ := repo.ListWaves(id); len(waves) != 2 {
		t.Fatalf("a refused claim left a wave behind: %+v", waves)
	}
	recipients, err := repo.WaveRecipients(id, 1)
	if err != nil || len(recipients) != 3 || recipients[0].PhoneNumber != "5584900000000" {
		t.Fatalf("recipients = %+v, %v, want the 3 oldest with their numbers", recipients, err)
	}
}

func TestWaveIntegration_PendingOutcomeAndPool(t *testing.T) {
	db := waveDB(t)
	repo := NewRepository(db)
	id := addCampaign(t, db, "RUNNING", 10)
	if _, err := repo.ClaimWave(id, 1, 10, at); err != nil {
		t.Fatal(err)
	}
	setEntries(t, db, id, 1, 4, "DELIVERED", 0)
	setEntries(t, db, id, 1, 2, "READ", 0)
	setEntries(t, db, id, 1, 1, "FAILED", sc.CodeEngagementLimit)

	pending, err := repo.PendingInWave(id, 1)
	if err != nil || pending != 3 {
		t.Fatalf("pending = %d, %v, want 3", pending, err)
	}
	outcome, err := repo.WaveOutcome(id, 1)
	if err != nil || outcome.Processed != 7 || outcome.Delivered != 6 || outcome.Read != 2 || outcome.Failures[sc.CodeEngagementLimit] != 1 {
		t.Fatalf("outcome = %+v, %v", outcome, err)
	}
	returned, err := repo.ReturnToPool(id, 1)
	if err != nil || returned != 3 {
		t.Fatalf("returned = %d, %v, want the 3 unsent", returned, err)
	}
	again, err := repo.ClaimWave(id, 2, 10, at)
	if err != nil || again.Size != 3 {
		t.Fatalf("reclaim = %+v, %v, want the returned 3", again, err)
	}
}

func TestWaveIntegration_WavesRoundTripAndCount(t *testing.T) {
	db := waveDB(t)
	repo := NewRepository(db)
	id := addCampaign(t, db, "RUNNING", 4)
	w, _ := repo.ClaimWave(id, 1, 4, at)
	if err := w.MarkEnqueued(); err != nil {
		t.Fatal(err)
	}
	if err := w.MarkDrained(at.Add(time.Minute), 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	decision := sc.Decision{Action: sc.SlowDown, NextSize: 2, Findings: []sc.Finding{{Rule: sc.RuleFailureRateSlow, Action: sc.SlowDown, Observed: 0.1, Limit: 0.08, At: at}}}
	w.MarkDecided(at.Add(2*time.Hour), sc.Outcome{Processed: 4, Delivered: 3, Failures: map[int]int{131026: 1}}, decision)
	if err := repo.SaveWave(w); err != nil {
		t.Fatal(err)
	}

	current, err := repo.CurrentWave(id)
	if err != nil || current == nil || current.State != sc.Decided || current.Decision.Findings[0].Rule != sc.RuleFailureRateSlow || !current.ObserveUntil.Equal(at.Add(time.Minute+2*time.Hour)) {
		t.Fatalf("current = %+v, %v", current, err)
	}
	if n, err := repo.ReleasedSince(id, at); err != nil || n != 1 {
		t.Fatalf("released since = %d, %v", n, err)
	}
	if n, _ := repo.ReleasedSince(id, at.Add(time.Second)); n != 0 {
		t.Fatalf("a later day counted %d waves", n)
	}
	if none, err := repo.CurrentWave(uuid.NewString()); err != nil || none != nil {
		t.Fatalf("current wave of a fresh campaign = %+v, %v", none, err)
	}
}

func TestWaveIntegration_PolicyAndStateRoundTrip(t *testing.T) {
	db := waveDB(t)
	repo := NewRepository(db)
	id := addCampaign(t, db, "RUNNING", 0)

	if p, err := repo.FindPolicy(id); err != nil || p != nil {
		t.Fatalf("policy of a plain campaign = %+v, %v", p, err)
	}
	policy := sc.PresetPolicy(sc.Aggressive, nil)
	if err := repo.SavePolicy(id, policy); err != nil {
		t.Fatal(err)
	}
	policy.MaxWavesPerDay = 4
	if err := repo.SavePolicy(id, policy); err != nil {
		t.Fatal(err)
	}
	if p, err := repo.FindPolicy(id); err != nil || p.Preset != sc.Aggressive || p.MaxWavesPerDay != 4 || p.ObserveWait != time.Hour {
		t.Fatalf("policy = %+v, %v", p, err)
	}

	if st, err := repo.FindState(id); err != nil || st.CampaignID != id || st.Hold != nil {
		t.Fatalf("fresh state = %+v, %v", st, err)
	}
	st := sc.State{CampaignID: id, UnreadSince: at, DueAt: at.Add(time.Hour)}
	st.Held(sc.Decision{Action: sc.Pause, Findings: []sc.Finding{{Rule: sc.RuleTemplateYellow, Action: sc.Pause, At: at}}}, at)
	st.WaitFor(sc.Decision{Action: sc.Wait, WaitUntil: at.Add(3 * time.Hour)}, at.Add(3*time.Hour))
	if err := repo.SaveState(st); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindState(id)
	if err != nil || got.Hold == nil || got.Hold.Decision.Findings[0].Rule != sc.RuleTemplateYellow || got.Waiting == nil || !got.DueAt.Equal(at.Add(3*time.Hour)) || !got.UnreadSince.Equal(at) {
		t.Fatalf("state = %+v, %v", got, err)
	}

	if err := repo.DeletePolicy(id); err != nil {
		t.Fatal(err)
	}
	if p, _ := repo.FindPolicy(id); p != nil {
		t.Fatal("the policy survived its deletion")
	}
}

func TestWaveIntegration_OnlyRunningSmartCampaignsAreDueOrMatched(t *testing.T) {
	db := waveDB(t)
	repo := NewRepository(db)
	due := addCampaign(t, db, "RUNNING", 0)
	later := addCampaign(t, db, "RUNNING", 0)
	paused := addCampaign(t, db, "PAUSED", 0)
	plain := addCampaign(t, db, "RUNNING", 0)
	for _, id := range []string{due, later, paused} {
		if err := repo.SavePolicy(id, sc.PresetPolicy(sc.Standard, nil)); err != nil {
			t.Fatal(err)
		}
	}
	repo.SaveState(sc.State{CampaignID: due, DueAt: at})
	repo.SaveState(sc.State{CampaignID: later, DueAt: at.Add(time.Hour)})
	repo.SaveState(sc.State{CampaignID: paused, DueAt: at})
	repo.SaveState(sc.State{CampaignID: plain, DueAt: at})

	ids, err := repo.DueCampaigns(at, 10)
	if err != nil || len(ids) != 1 || ids[0] != due {
		t.Fatalf("due = %v, %v, want only the running smart campaign that is due", ids, err)
	}

	var template, phone string
	db.Raw(`SELECT template_id::text FROM whatsapp_campaigns WHERE id = ?`, later).Scan(&template)
	db.Raw(`SELECT business_phone_id::text FROM whatsapp_campaigns WHERE id = ?`, due).Scan(&phone)
	if ids, err := repo.RunningUsing(template, ""); err != nil || len(ids) != 1 || ids[0] != later {
		t.Fatalf("by template = %v, %v", ids, err)
	}
	if ids, err := repo.RunningUsing("", phone); err != nil || len(ids) != 1 || ids[0] != due {
		t.Fatalf("by phone = %v, %v", ids, err)
	}
	if ids, err := repo.RunningUsing("", ""); err != nil || len(ids) != 0 {
		t.Fatalf("no subject = %v, %v", ids, err)
	}
}
