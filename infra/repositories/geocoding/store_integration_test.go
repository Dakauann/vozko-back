package geocoding_repository

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/geocoding"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestConcurrentSlotsNeverPassTheLimitAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "geocoding_usage", &schema.GeocodingUsage{}, &schema.GeocodingUsageMonth{}, &schema.GeocodingSettings{})
	store := NewUsageStore(db)
	ws := uuid.NewString()
	settingsOf(t, db, ws, "opencage", 10)
	s := slot()
	s.DailyLimit = 7
	var taken atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, _, err := store.TakeSlot(context.Background(), ws, s)
			if err != nil {
				t.Errorf("TakeSlot() err = %v", err)
			}
			if ok {
				taken.Add(1)
			}
		}()
	}
	wg.Wait()
	if taken.Load() != 7 {
		t.Fatalf("taken %d slots, want exactly the daily limit of 7", taken.Load())
	}
	ok, usage, err := store.TakeSlot(context.Background(), ws, s)
	if err != nil || ok {
		t.Fatalf("a slot past the day = %v, %v", ok, err)
	}
	if refusal, until := s.Refusal(usage); refusal != geocoding.ExhaustedDaily || !until.Equal(s.NextDay) {
		t.Fatalf("Refusal() = %q until %v, want the day spent", refusal, until)
	}

	tomorrow := s
	tomorrow.Day, tomorrow.NextDay = s.NextDay, s.NextDay.AddDate(0, 0, 1)
	for i := 0; i < 3; i++ {
		if ok, _, err := store.TakeSlot(context.Background(), ws, tomorrow); err != nil || !ok {
			t.Fatalf("tomorrow's slot %d = %v, %v", i, ok, err)
		}
	}
	if ok, usage, _ := store.TakeSlot(context.Background(), ws, tomorrow); ok || usage.Requests != 10 {
		t.Fatalf("the month = %v with %d requests, want refused at the monthly limit of 10", ok, usage.Requests)
	}
	nextMonth := tomorrow
	nextMonth.CycleStart, nextMonth.Day = s.NextCycle, s.NextCycle
	if ok, usage, err := store.TakeSlot(context.Background(), ws, nextMonth); err != nil || !ok || usage.Requests != 1 || usage.DayRequests != 1 {
		t.Fatalf("a new cycle = %v, %+v, %v, want the counters reset", ok, usage, err)
	}
}

func TestSettingsChangesRoundTripAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "geocoding_settings", &schema.GeocodingSettings{})
	store := NewSettingsStore(db)
	ws, owner := uuid.NewString(), uuid.NewString()
	now := time.Now().UTC().Truncate(time.Second)
	configured := func(geocoding.Provider) bool { return true }
	opencage := geocoding.ProviderOpenCage
	saved, err := store.ChangeSettings(context.Background(), ws, func(s geocoding.Settings) (geocoding.Settings, error) {
		return s.Apply(geocoding.Change{Provider: &opencage}, geocoding.Editor{UserID: owner, OwnerID: owner}, configured, now)
	})
	if err != nil || !saved.Enabled() {
		t.Fatalf("ChangeSettings() = %+v, %v", saved, err)
	}
	read, err := store.Settings(context.Background(), ws)
	if err != nil || read.Provider != opencage || read.ProviderChangedBy != owner || read.ProviderChangedAt == nil || !read.ProviderChangedAt.Equal(now) {
		t.Fatalf("Settings() = %+v, %v, want the opt-in with who and when", read, err)
	}
	many, err := store.SettingsOf(context.Background(), []string{ws, uuid.NewString()})
	if err != nil || len(many) != 1 || !many[ws].Enabled() {
		t.Fatalf("SettingsOf() = %+v, %v", many, err)
	}
	refused := geocoding.ErrCeilingForbidden
	ceiling := int64(10)
	if _, err := store.ChangeSettings(context.Background(), ws, func(s geocoding.Settings) (geocoding.Settings, error) {
		return s.Apply(geocoding.Change{MonthlyCeiling: &ceiling}, geocoding.Editor{UserID: owner, OwnerID: owner}, configured, now)
	}); !errors.Is(err, refused) {
		t.Fatalf("ChangeSettings() err = %v, want %v", err, refused)
	}
	if again, _ := store.Settings(context.Background(), ws); again.MonthlyCeiling != nil {
		t.Fatalf("a refused change was written: %+v", again)
	}
}
