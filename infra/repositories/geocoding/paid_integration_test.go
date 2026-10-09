package geocoding_repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/geo"
	"vozko/domain/geocoding"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func settingsOf(t *testing.T, db *gorm.DB, ws, provider string, ceiling int64) {
	t.Helper()
	if err := db.Exec("INSERT INTO geocoding_settings (workspace_id, provider, monthly_ceiling, updated_at) VALUES (?, ?, ?, now())", ws, provider, ceiling).Error; err != nil {
		t.Fatalf("settings: %v", err)
	}
}

func TestASlotIsRefusedTheMomentTheSettingsChangeAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "geocoding_terms", &schema.GeocodingUsage{}, &schema.GeocodingUsageMonth{}, &schema.GeocodingSettings{})
	usage, settings := NewUsageStore(db), NewSettingsStore(db)
	ctx := context.Background()
	ws, admin := uuid.NewString(), uuid.NewString()
	now := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	opencage := geocoding.ProviderOpenCage
	configured := func(geocoding.Provider) bool { return true }
	change := func(c geocoding.Change) geocoding.Settings {
		t.Helper()
		saved, err := settings.ChangeSettings(ctx, ws, func(s geocoding.Settings) (geocoding.Settings, error) {
			return s.Apply(c, geocoding.Editor{UserID: admin, OwnerID: admin, PlatformAdmin: true}, configured, now)
		})
		if err != nil {
			t.Fatal(err)
		}
		return saved
	}
	slotOf := func(s geocoding.Settings) geocoding.Slot {
		t.Helper()
		slot, err := s.SlotAt(now)
		if err != nil {
			t.Fatal(err)
		}
		return slot
	}

	read := slotOf(change(geocoding.Change{Provider: &opencage}))
	if ok, _, err := usage.TakeSlot(ctx, ws, read); err != nil || !ok {
		t.Fatalf("a slot under the default ceiling = %v, %v", ok, err)
	}

	lower := int64(40)
	lowered := change(geocoding.Change{MonthlyCeiling: &lower})
	if ok, _, err := usage.TakeSlot(ctx, ws, read); err != nil || ok {
		t.Fatalf("a slot computed before the ceiling was lowered = %v, %v, want refused at once", ok, err)
	}
	if ok, _, err := usage.TakeSlot(ctx, ws, slotOf(lowered)); err != nil || !ok {
		t.Fatalf("a slot under the new ceiling = %v, %v", ok, err)
	}

	off := geocoding.Provider("")
	change(geocoding.Change{Provider: &off})
	if ok, _, err := usage.TakeSlot(ctx, ws, slotOf(lowered)); err != nil || ok {
		t.Fatalf("a slot after the provider was switched off = %v, %v, want refused at once", ok, err)
	}
	if ok, _, err := usage.TakeSlot(ctx, uuid.NewString(), slotOf(lowered)); err != nil || ok {
		t.Fatalf("a slot for a workspace without settings = %v, %v, want refused", ok, err)
	}
	current, err := usage.Usage(ctx, ws)
	if err != nil || current.Requests != 2 {
		t.Fatalf("usage = %+v, %v, want only the two slots taken under current settings", current, err)
	}
}

func answersDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "geocode_cache")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.Lead{}, &schema.LeadAddress{}, &schema.GeocodeCache{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func holdText(t *testing.T, db *gorm.DB, ws, fingerprint string, deleted bool) string {
	t.Helper()
	leadID := uuid.NewString()
	var deletedAt any
	if deleted {
		deletedAt = time.Now().UTC()
	}
	if err := db.Exec("INSERT INTO leads (id, workspace_id, created_at, updated_at, deleted_at) VALUES (?, ?, now(), now(), ?)", leadID, ws, deletedAt).Error; err != nil {
		t.Fatalf("lead: %v", err)
	}
	if err := db.Exec("INSERT INTO lead_addresses (id, workspace_id, lead_id, label, is_primary, geo_status, fingerprint, created_at, updated_at)"+
		" VALUES (?, ?, ?, 'home', true, 'pending', ?, now(), now())", uuid.NewString(), ws, leadID, fingerprint).Error; err != nil {
		t.Fatalf("address: %v", err)
	}
	return leadID
}

func TestAnswersRoundTripPerWorkspaceAgainstPostgres(t *testing.T) {
	db := answersDB(t)
	store := NewAnswerStore(db)
	ctx := context.Background()
	ws, other := uuid.NewString(), uuid.NewString()
	holdText(t, db, ws, "f-located", false)
	holdText(t, db, ws, "f-refused", false)
	holdText(t, db, other, "f-located", false)

	located := locatedAnswer(ws, "f-located")
	refused, _ := geocoding.AnswerOf(geocoding.ProviderOpenCage, geocoding.AnswerKey{WorkspaceID: ws, Fingerprint: "f-refused"}, geo.Unavailable(geo.ReasonQueryRefused, 0), answeredAt)
	for _, a := range []geocoding.Answer{located, refused} {
		if err := store.Remember(ctx, a); err != nil {
			t.Fatalf("Remember(%s): %v", a.Kind, err)
		}
	}
	better, _ := geocoding.AnswerOf(geocoding.ProviderOpenCage, located.Key, geo.NotFound(), answeredAt.Add(time.Hour))
	if err := store.Remember(ctx, better); err != nil {
		t.Fatalf("a newer answer for the same text: %v", err)
	}

	got, err := store.Answers(ctx, []geocoding.AnswerKey{located.Key, refused.Key, {WorkspaceID: other, Fingerprint: "f-located"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Answers() = %+v, want two answers and none for the other workspace", got)
	}
	if a := got[located.Key]; a.Kind != geocoding.AnswerNotFound || a.Fix != nil || !a.ResolvedAt.Equal(answeredAt.Add(time.Hour)) {
		t.Fatalf("answer = %+v, want the newer no-result answer", a)
	}
	if a := got[refused.Key]; a.Kind != geocoding.AnswerRefused {
		t.Fatalf("answer = %+v, want the refused text", a)
	}

	if err := store.Remember(ctx, locatedAnswer(ws, "f-located")); err != nil {
		t.Fatal(err)
	}
	again, _ := store.Answers(ctx, []geocoding.AnswerKey{located.Key})
	if a := again[located.Key]; a.Kind != geocoding.AnswerLocated || a.Fix == nil || a.Fix.Precision != geo.PrecisionAddress || a.Fix.Point.Lat != -23.5613 {
		t.Fatalf("answer = %+v, want the located position read back", a)
	}
}

func TestAnAnswerIsNeverStoredForATextNoLiveLeadHoldsAgainstPostgres(t *testing.T) {
	db := answersDB(t)
	store := NewAnswerStore(db)
	ctx := context.Background()
	ws := uuid.NewString()
	holdText(t, db, ws, "f-gone", true)
	holdText(t, db, uuid.NewString(), "f-elsewhere", false)
	for _, fingerprint := range []string{"f-gone", "f-elsewhere", "f-nobody"} {
		if err := store.Remember(ctx, locatedAnswer(ws, fingerprint)); err != nil {
			t.Fatalf("Remember(%s): %v", fingerprint, err)
		}
	}
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM geocode_cache").Scan(&n).Error; err != nil || n != 0 {
		t.Fatalf("cache rows = %d, %v, want nothing kept for a text no live lead of the workspace holds", n, err)
	}
}

func TestAnAnswerArrivingDuringAnAnonymizationIsNotKeptAgainstPostgres(t *testing.T) {
	db := answersDB(t)
	store := NewAnswerStore(db)
	ws := uuid.NewString()
	leadID := holdText(t, db, ws, "f-1", false)
	tx := db.Begin()
	defer tx.Rollback()
	if err := tx.Exec("SELECT id FROM leads WHERE id = ? FOR UPDATE", leadID).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("UPDATE leads SET deleted_at = now() WHERE id = ?", leadID).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("DELETE FROM lead_addresses WHERE lead_id = ?", leadID).Error; err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- store.Remember(context.Background(), locatedAnswer(ws, "f-1")) }()
	select {
	case err := <-done:
		t.Fatalf("Remember() returned %v while the lead was being anonymized, want it to wait for the lock", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM geocode_cache").Scan(&n).Error; err != nil || n != 0 {
		t.Fatalf("cache rows = %d, %v, want the answer dropped once the only holder was anonymized", n, err)
	}
}

func TestAnAnswerArrivingDuringAnAddressEditIsNotKeptAgainstPostgres(t *testing.T) {
	edits := map[string]string{
		"the text changes": "UPDATE lead_addresses SET fingerprint = 'f-new', updated_at = now() WHERE lead_id = ?",
		"the address goes": "DELETE FROM lead_addresses WHERE lead_id = ?",
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			db := answersDB(t)
			store := NewAnswerStore(db)
			ws := uuid.NewString()
			leadID := holdText(t, db, ws, "f-1", false)
			tx := db.Begin()
			defer tx.Rollback()
			if err := tx.Exec("UPDATE leads SET version = version + 1, updated_at = now() WHERE id = ?", leadID).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Exec(edit, leadID).Error; err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- store.Remember(context.Background(), locatedAnswer(ws, "f-1")) }()
			select {
			case err := <-done:
				t.Fatalf("Remember() returned %v while the address was being edited, want it to wait for the edit", err)
			case <-time.After(300 * time.Millisecond):
			}
			if err := tx.Commit().Error; err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			var n int64
			if err := db.Raw("SELECT COUNT(*) FROM geocode_cache").Scan(&n).Error; err != nil || n != 0 {
				t.Fatalf("cache rows = %d, %v, want the answer dropped once no address holds the text", n, err)
			}
		})
	}
}

func TestAnAnswerWaitingTooLongForTheHolderIsReportedNotStoredAgainstPostgres(t *testing.T) {
	db := answersDB(t)
	store := NewAnswerStore(db)
	ws := uuid.NewString()
	leadID := holdText(t, db, ws, "f-1", false)
	tx := db.Begin()
	defer tx.Rollback()
	if err := tx.Exec("SELECT id FROM leads WHERE id = ? FOR UPDATE", leadID).Error; err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := store.Remember(context.Background(), locatedAnswer(ws, "f-1")); err == nil {
		t.Fatal("an answer that waited past the lock timeout must be reported as not stored")
	}
	if waited := time.Since(started); waited > 10*time.Second {
		t.Fatalf("Remember() waited %v, want it bounded by the lock timeout", waited)
	}
}
