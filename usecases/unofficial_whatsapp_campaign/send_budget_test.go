package unofficial_whatsapp_campaign

import (
	"testing"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

func newBudget(t *testing.T) (*SendBudget, *fakeShared) {
	t.Helper()
	shared := newFakeShared()
	b := NewSendBudget(shared)
	b.now = func() time.Time { return time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC) }
	return b, shared
}

func TestDailyCapStopsAtTheCeiling(t *testing.T) {
	b, _ := newBudget(t)
	for i := 1; i <= 3; i++ {
		ok, err := b.TryConsumeDaily("inst-1", 3)
		if err != nil || !ok {
			t.Fatalf("send %d refused: ok=%v err=%v", i, ok, err)
		}
	}
	ok, err := b.TryConsumeDaily("inst-1", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("the 4th send was allowed past a cap of 3")
	}
}

// A refusal must not leave the counter permanently above the cap, or a paused
// campaign would lock the number out for the rest of the day.
func TestRefusalDoesNotOvercount(t *testing.T) {
	b, _ := newBudget(t)
	for i := 0; i < 2; i++ {
		b.TryConsumeDaily("inst-1", 2)
	}
	for i := 0; i < 5; i++ {
		b.TryConsumeDaily("inst-1", 2)
	}
	if got := b.UsedToday("inst-1"); got != 2 {
		t.Fatalf("used = %d after repeated refusals, want 2", got)
	}
}

// A budgeted send that did not happen returns its reservation. Without this a
// 30%-dead list burns 30% of the number's allowance on nobody.
func TestReleaseReturnsAnUnspentReservation(t *testing.T) {
	b, _ := newBudget(t)
	b.TryConsumeDaily("inst-1", 5)
	b.TryConsumeDaily("inst-1", 5)
	b.ReleaseDaily("inst-1", 5)
	if got := b.UsedToday("inst-1"); got != 1 {
		t.Fatalf("used = %d after one release, want 1", got)
	}
}

// A cache we cannot reach is not permission to blast.
func TestBudgetFailsClosedOnCacheError(t *testing.T) {
	shared := newFakeShared()
	shared.failIncr = true
	b := NewSendBudget(shared)
	ok, err := b.TryConsumeDaily("inst-1", 10)
	if ok {
		t.Fatal("a send was allowed while the cache was unreachable")
	}
	if err == nil {
		t.Fatal("expected the cache error to surface")
	}
}

func TestUnlimitedCapSkipsTheCounter(t *testing.T) {
	b, _ := newBudget(t)
	ok, err := b.TryConsumeDaily("inst-1", 0)
	if !ok || err != nil {
		t.Fatalf("cap 0 refused: ok=%v err=%v", ok, err)
	}
	if got := b.UsedToday("inst-1"); got != 0 {
		t.Fatalf("an unlimited cap still consumed budget: %d", got)
	}
}

// The lease is per NUMBER, not per process: two campaigns on one instance must
// not both send at once.
func TestPaceLeaseIsExclusivePerInstance(t *testing.T) {
	b, _ := newBudget(t)
	b.jitter = func(minMS, maxMS int) int { return minMS }

	ok, _ := b.AcquirePace("inst-1", 1000, 2000)
	if !ok {
		t.Fatal("the first caller did not get the lease")
	}
	ok, wait := b.AcquirePace("inst-1", 1000, 2000)
	if ok {
		t.Fatal("a second caller got the lease while it was held")
	}
	if wait <= 0 {
		t.Fatal("a refused caller was given no wait")
	}

	// A different number is independent.
	if ok, _ := b.AcquirePace("inst-2", 1000, 2000); !ok {
		t.Fatal("a different number was blocked by another number's lease")
	}
}

// A campaign may be slower than its number but never faster: the ban risk
// belongs to the number.
func TestPacingNeverGoesBelowTheInstanceFloor(t *testing.T) {
	instance := &uw.Instance{SendDelayMinMS: 5000, SendDelayMaxMS: 9000}
	camp := &uwc.Campaign{SendDelayMinMS: 600, SendDelayMaxMS: 900}

	min, max := PacingFor(camp, instance)
	if min < 5000 {
		t.Fatalf("min = %d, below the instance floor of 5000", min)
	}
	if max < min {
		t.Fatalf("range inverted: %d..%d", min, max)
	}
}

func TestPacingAllowsACampaignToBeSlower(t *testing.T) {
	instance := &uw.Instance{SendDelayMinMS: 3000, SendDelayMaxMS: 12000}
	camp := &uwc.Campaign{SendDelayMinMS: 20000, SendDelayMaxMS: 40000}

	min, max := PacingFor(camp, instance)
	if min != 20000 || max != 40000 {
		t.Fatalf("range = %d..%d, want the campaign's slower window", min, max)
	}
}

// The jitter must actually vary, or the cadence is machine-regular — the exact
// signature the whole control exists to avoid.
func TestJitterVaries(t *testing.T) {
	seen := map[int]bool{}
	for i := 0; i < 200; i++ {
		seen[defaultJitter(1000, 1010)] = true
	}
	if len(seen) < 3 {
		t.Fatalf("jitter produced only %d distinct delays", len(seen))
	}
	for d := range seen {
		if d < 1000 || d > 1010 {
			t.Fatalf("jitter %d escaped the range", d)
		}
	}
}
