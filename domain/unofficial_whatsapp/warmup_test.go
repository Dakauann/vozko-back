package unofficial_whatsapp

import (
	"testing"
	"time"
)

func TestEffectiveDailyCapWithoutWarmup(t *testing.T) {
	now := time.Now().UTC()

	// A zero cap means UNKNOWN, not unlimited. Guessing unlimited means blasting
	// a number nobody has configured a limit for.
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

	// Day 1 is the day warmup started, so a number that just started gets 1/21
	// rather than nothing.
	inst.WarmupStartedAt = at(0)
	if got := inst.EffectiveDailyCap(now); got != 100 {
		t.Errorf("day 1 = %d, want 100", got)
	}

	inst.WarmupStartedAt = at(10)
	if got, want := inst.EffectiveDailyCap(now), 2100*11/WarmupDays; got != want {
		t.Errorf("day 11 = %d, want %d", got, want)
	}

	// Past the ramp, the full cap.
	inst.WarmupStartedAt = at(WarmupDays + 5)
	if got := inst.EffectiveDailyCap(now); got != 2100 {
		t.Errorf("post-warmup = %d, want the full 2100", got)
	}
}

// A brand-new number must still be able to do useful work on day one.
func TestWarmupHasAFloor(t *testing.T) {
	now := time.Now().UTC()
	start := now
	inst := &Instance{DailySendCap: 100, WarmupStartedAt: &start}
	if got := inst.EffectiveDailyCap(now); got != MinWarmupDailyCap {
		t.Fatalf("day 1 of a small cap = %d, want the floor %d", got, MinWarmupDailyCap)
	}
}

// A cap deliberately set BELOW the warmup floor is a restriction and must win:
// the floor exists to help a new number, not to override an operator who asked
// for less.
func TestAConfiguredCapBelowTheFloorWins(t *testing.T) {
	now := time.Now().UTC()
	start := now
	inst := &Instance{DailySendCap: 10, WarmupStartedAt: &start}
	if got := inst.EffectiveDailyCap(now); got != 10 {
		t.Fatalf("cap = %d, want the configured 10", got)
	}
}

// A warmup start stamped in the future is a misconfiguration, not permission to
// send at full rate.
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
