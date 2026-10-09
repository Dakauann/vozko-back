package geocoding

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/billing"
)

func ceiling(v int64) *int64 { return &v }

func provider(p Provider) *Provider { return &p }

func configured(p Provider) bool { return p == ProviderOpenCage }

func TestSettingsCeiling(t *testing.T) {
	tests := []struct {
		name     string
		settings Settings
		want     int64
	}{
		{"no provider means no external calls", Settings{}, 0},
		{"no provider keeps zero even with a ceiling set", Settings{MonthlyCeiling: ceiling(900)}, 0},
		{"an enabled provider gets the default ceiling", Settings{Provider: ProviderOpenCage}, DefaultMonthlyCeiling},
		{"an explicit ceiling wins", Settings{Provider: ProviderOpenCage, MonthlyCeiling: ceiling(1200)}, 1200},
		{"an explicit zero stops external calls", Settings{Provider: ProviderOpenCage, MonthlyCeiling: ceiling(0)}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.settings.Ceiling(); got != tt.want {
				t.Fatalf("Ceiling() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSettingsSlotAt(t *testing.T) {
	brt := billing.LocationBRT()
	now := time.Date(2026, 10, 8, 2, 30, 0, 0, time.UTC)
	t.Run("a provider that is off has no slot", func(t *testing.T) {
		if _, err := (Settings{}).SlotAt(now); !errors.Is(err, ErrProviderOff) {
			t.Fatalf("SlotAt() err = %v, want ErrProviderOff", err)
		}
	})
	t.Run("a zero ceiling has no slot", func(t *testing.T) {
		if _, err := (Settings{Provider: ProviderOpenCage, MonthlyCeiling: ceiling(0)}).SlotAt(now); !errors.Is(err, ErrNoRoom) {
			t.Fatalf("SlotAt() err = %v, want ErrNoRoom", err)
		}
	})
	t.Run("the default ceiling is shared across the days of the cycle", func(t *testing.T) {
		slot, err := Settings{Provider: ProviderOpenCage}.SlotAt(now)
		if err != nil {
			t.Fatalf("SlotAt() err = %v", err)
		}
		if !slot.CycleStart.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, brt)) || !slot.NextCycle.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, brt)) {
			t.Fatalf("cycle = %v to %v, want October in Brasilia time", slot.CycleStart, slot.NextCycle)
		}
		if !slot.Day.Equal(time.Date(2026, 10, 7, 0, 0, 0, 0, brt)) || !slot.NextDay.Equal(time.Date(2026, 10, 8, 0, 0, 0, 0, brt)) {
			t.Fatalf("day = %v to %v, want the Brasilia day (02:30 UTC is still the 7th)", slot.Day, slot.NextDay)
		}
		if slot.MonthlyLimit != 5000 || slot.DailyLimit != 323 {
			t.Fatalf("limits = %d monthly, %d daily, want 5000 and 323 (twice an even share of 31 days)", slot.MonthlyLimit, slot.DailyLimit)
		}
	})
	t.Run("a tiny ceiling still allows one call a day", func(t *testing.T) {
		slot, err := Settings{Provider: ProviderOpenCage, MonthlyCeiling: ceiling(3)}.SlotAt(now)
		if err != nil || slot.DailyLimit != 1 {
			t.Fatalf("SlotAt() = %+v, %v, want a daily limit of 1", slot, err)
		}
	})
}

func TestSlotRefusal(t *testing.T) {
	now := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	slot, err := Settings{Provider: ProviderOpenCage, MonthlyCeiling: ceiling(100)}.SlotAt(now)
	if err != nil {
		t.Fatal(err)
	}
	stale := Usage{CycleStart: slot.CycleStart.AddDate(0, -1, 0), Requests: 100, Day: slot.Day.AddDate(0, 0, -1), DayRequests: 7}
	tests := []struct {
		name  string
		usage Usage
		want  Exhaustion
		until time.Time
	}{
		{"room left", Usage{CycleStart: slot.CycleStart, Requests: 10, Day: slot.Day, DayRequests: 2}, ExhaustedNone, time.Time{}},
		{"the month is spent", Usage{CycleStart: slot.CycleStart, Requests: 100, Day: slot.Day, DayRequests: 2}, ExhaustedMonthly, slot.NextCycle},
		{"the day is spent", Usage{CycleStart: slot.CycleStart, Requests: 50, Day: slot.Day, DayRequests: slot.DailyLimit}, ExhaustedDaily, slot.NextDay},
		{"counters of an older cycle and day do not count", stale, ExhaustedNone, time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, until := slot.Refusal(tt.usage)
			if got != tt.want || !until.Equal(tt.until) {
				t.Fatalf("Refusal() = %q until %v, want %q until %v", got, until, tt.want, tt.until)
			}
		})
	}
}

func TestASlotCarriesTheSettingsItWasComputedFrom(t *testing.T) {
	now := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	set, _ := Settings{Provider: ProviderOpenCage, MonthlyCeiling: ceiling(100)}.SlotAt(now)
	if set.Provider != ProviderOpenCage || set.StoredCeiling == nil || *set.StoredCeiling != 100 {
		t.Fatalf("slot = %+v, want the provider and the stored ceiling it was computed from", set)
	}
	unset, _ := Settings{Provider: ProviderOpenCage}.SlotAt(now)
	if unset.Provider != ProviderOpenCage || unset.StoredCeiling != nil || unset.MonthlyLimit != DefaultMonthlyCeiling {
		t.Fatalf("slot = %+v, want no stored ceiling under the default", unset)
	}
}

func TestARefusedSlotSaysWhyTheCallDidNotHappen(t *testing.T) {
	now := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	slot, err := Settings{Provider: ProviderOpenCage, MonthlyCeiling: ceiling(100)}.SlotAt(now)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		usage      Usage
		exhaustion Exhaustion
		step       ProviderStep
	}{
		{"the month is spent", Usage{CycleStart: slot.CycleStart, Requests: 100, Day: slot.Day}, ExhaustedMonthly, QuotaReached(slot.NextCycle)},
		{"the day is spent", Usage{CycleStart: slot.CycleStart, Requests: 5, Day: slot.Day, DayRequests: slot.DailyLimit}, ExhaustedDaily, QuotaReached(slot.NextDay)},
		{"room left means the settings moved since they were read", Usage{CycleStart: slot.CycleStart, Requests: 5, Day: slot.Day, DayRequests: 1}, ExhaustedNone, Deferred()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step, exhaustion := slot.Refused(tt.usage)
			if exhaustion != tt.exhaustion || step.kind != tt.step.kind || !step.Until.Equal(tt.step.Until) || step.Called() {
				t.Fatalf("Refused() = %+v, %q, want %+v, %q", step, exhaustion, tt.step, tt.exhaustion)
			}
		})
	}
}

func TestSettingsApply(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	owner := Editor{UserID: "owner-1", OwnerID: "owner-1"}
	staff := Editor{UserID: "staff-1", PlatformAdmin: true}
	member := Editor{UserID: "member-1", OwnerID: "owner-1"}
	enabled := Settings{Provider: ProviderOpenCage, ProviderChangedBy: "owner-1", ProviderChangedAt: &now}

	tests := []struct {
		name    string
		current Settings
		change  Change
		editor  Editor
		wantErr error
		check   func(t *testing.T, got Settings)
	}{
		{"an empty change is refused", Settings{}, Change{}, owner, ErrChangeEmpty, nil},
		{"a change without an actor is refused", Settings{}, Change{Provider: provider(ProviderOpenCage)}, Editor{OwnerID: "owner-1"}, ErrSettingsForbidden, nil},
		{"a member who is not the owner cannot opt in", Settings{}, Change{Provider: provider(ProviderOpenCage)}, member, ErrSettingsForbidden, nil},
		{"an unknown provider is refused", Settings{}, Change{Provider: provider("google")}, owner, ErrProviderUnknown, nil},
		{"a provider this server cannot reach is refused", Settings{}, Change{Provider: provider(ProviderOpenCage)}, owner, ErrProviderNotConfigured, nil},
		{"the owner opts in and it is recorded", Settings{}, Change{Provider: provider(ProviderOpenCage)}, owner, nil, func(t *testing.T, got Settings) {
			if got.Provider != ProviderOpenCage || got.ProviderChangedBy != "owner-1" || got.ProviderChangedAt == nil || !got.ProviderChangedAt.Equal(now) {
				t.Fatalf("Apply() = %+v, want the opt-in stamped by the owner", got)
			}
		}},
		{"a platform admin opts out", enabled, Change{Provider: provider("")}, staff, nil, func(t *testing.T, got Settings) {
			if got.Enabled() || got.ProviderChangedBy != "staff-1" {
				t.Fatalf("Apply() = %+v, want the opt-out stamped by staff", got)
			}
		}},
		{"repeating the provider keeps the stamp", enabled, Change{Provider: provider(ProviderOpenCage)}, staff, nil, func(t *testing.T, got Settings) {
			if got.ProviderChangedBy != "owner-1" {
				t.Fatalf("Apply() = %+v, want the original stamp kept", got)
			}
		}},
		{"the owner cannot change the ceiling", enabled, Change{MonthlyCeiling: ceiling(9000)}, owner, ErrCeilingForbidden, nil},
		{"a negative ceiling is refused", enabled, Change{MonthlyCeiling: ceiling(-1)}, staff, ErrCeilingInvalid, nil},
		{"a ceiling above the platform maximum is refused", enabled, Change{MonthlyCeiling: ceiling(MaxMonthlyCeiling + 1)}, staff, ErrCeilingInvalid, nil},
		{"a platform admin sets the ceiling and it is recorded", enabled, Change{MonthlyCeiling: ceiling(1200)}, staff, nil, func(t *testing.T, got Settings) {
			if got.Ceiling() != 1200 || got.CeilingChangedBy != "staff-1" || got.CeilingChangedAt == nil {
				t.Fatalf("Apply() = %+v, want the ceiling stamped by staff", got)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reachable := configured
			if tt.wantErr == ErrProviderNotConfigured {
				reachable = func(Provider) bool { return false }
			}
			got, err := tt.current.Apply(tt.change, tt.editor, reachable, now)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Apply() err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Apply() err = %v", err)
			}
			tt.check(t, got)
		})
	}
}

func TestEditorRights(t *testing.T) {
	if !(Editor{UserID: "u", OwnerID: "u"}).CanChangeProvider() || (Editor{UserID: "u", OwnerID: "u"}).CanChangeCeiling() {
		t.Fatal("the owner changes the provider but not the ceiling")
	}
	if !(Editor{UserID: "u", PlatformAdmin: true}).CanChangeCeiling() {
		t.Fatal("a platform admin changes the ceiling")
	}
	if (Editor{UserID: "m", OwnerID: "u"}).CanChangeProvider() {
		t.Fatal("a member who is not the owner does not change the provider")
	}
	if (Editor{PlatformAdmin: true}).CanChangeProvider() {
		t.Fatal("an editor without an id changes nothing")
	}
}
