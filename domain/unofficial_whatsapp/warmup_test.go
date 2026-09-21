package unofficial_whatsapp

import (
	"testing"
	"time"
)

func TestEffectiveDailyCapWithoutWarmup(t *testing.T) {
	now := time.Now().UTC()

	if got := (&Instance{}).EffectiveDailyCap(now); got != DefaultDailySendCap {
		t.Fatalf("unset cap = %d, want the default %d", got, DefaultDailySendCap)
	}
	if got := (&Instance{DailySendCap: 250}).EffectiveDailyCap(now); got != 250 {
		t.Fatalf("configured cap = %d, want 250", got)
	}
}

func TestWarmupRamps(t *testing.T) {
	now := time.Now().UTC()
	at := func(daysAgo int) *time.Time {
		v := now.AddDate(0, 0, -daysAgo)
		return &v
	}

	inst := &Instance{DailySendCap: 2100}

	inst.WarmupStartedAt = at(0)
	if got := inst.EffectiveDailyCap(now); got != 100 {
		t.Errorf("day 1 = %d, want 100", got)
	}

	inst.WarmupStartedAt = at(10)
	if got, want := inst.EffectiveDailyCap(now), 2100*11/WarmupDays; got != want {
		t.Errorf("day 11 = %d, want %d", got, want)
	}

	inst.WarmupStartedAt = at(WarmupDays + 5)
	if got := inst.EffectiveDailyCap(now); got != 2100 {
		t.Errorf("post-warmup = %d, want the full 2100", got)
	}
}

func TestWarmupHasAFloor(t *testing.T) {
	now := time.Now().UTC()
	start := now
	inst := &Instance{DailySendCap: 100, WarmupStartedAt: &start}
	if got := inst.EffectiveDailyCap(now); got != MinWarmupDailyCap {
		t.Fatalf("day 1 of a small cap = %d, want the floor %d", got, MinWarmupDailyCap)
	}
}

func TestAConfiguredCapBelowTheFloorWins(t *testing.T) {
	now := time.Now().UTC()
	start := now
	inst := &Instance{DailySendCap: 10, WarmupStartedAt: &start}
	if got := inst.EffectiveDailyCap(now); got != 10 {
		t.Fatalf("cap = %d, want the configured 10", got)
	}
}

func TestFutureWarmupStartDoesNotUnlockFullRate(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(48 * time.Hour)
	inst := &Instance{DailySendCap: 2100, WarmupStartedAt: &future}
	if got := inst.EffectiveDailyCap(now); got >= 2100 {
		t.Fatalf("future warmup start yielded %d, want a ramped value", got)
	}
}

func TestInWarmup(t *testing.T) {
	now := time.Now().UTC()
	recent := now.AddDate(0, 0, -3)
	old := now.AddDate(0, 0, -WarmupDays-1)

	if (&Instance{}).InWarmup(now) {
		t.Error("an instance with no warmup start reported as warming up")
	}
	if !(&Instance{WarmupStartedAt: &recent}).InWarmup(now) {
		t.Error("a 3-day-old number reported as finished warming up")
	}
	if (&Instance{WarmupStartedAt: &old}).InWarmup(now) {
		t.Error("a number past the ramp reported as still warming up")
	}
}
