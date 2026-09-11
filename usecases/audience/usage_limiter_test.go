package audience_usecase

import (
	"context"
	"testing"
	"time"

	ca "vozko/domain/audience"
)

// The rolling budget, and the three things the calendar-day counter it
// replaced could not do.

func TestUsageLimiterRefusesWhatDoesNotFit(t *testing.T) {
	l := NewUsageLimiter(newFakeState())
	ctx := context.Background()

	if ok, _ := l.Claim(ctx, "ws-1", 15, 20, now); !ok {
		t.Fatal("a claim under the budget must pass")
	}
	if ok, _ := l.Claim(ctx, "ws-1", 10, 20, now); ok {
		t.Fatal("a claim that would cross the budget must be refused")
	}
	if ok, _ := l.Claim(ctx, "ws-1", 5, 20, now); !ok {
		t.Fatal("what still fits must pass")
	}
	// Another workspace has its own budget.
	if ok, _ := l.Claim(ctx, "ws-2", 20, 20, now); !ok {
		t.Fatal("budgets are per workspace")
	}
}

// The window MOVES. This is the whole point: the counter this replaced handed
// back the entire allowance at UTC midnight, so a workspace could spend its day
// twice over by timing a burst either side of a reset nobody could see.
func TestUsageLimiterWindowRollsRatherThanResetting(t *testing.T) {
	l := NewUsageLimiter(newFakeState())
	ctx := context.Background()

	if ok, _ := l.Claim(ctx, "ws-1", 20, 20, now); !ok {
		t.Fatal("the first claim must fit")
	}
	// One hour later the budget is still spent: nothing has aged out.
	if ok, _ := l.Claim(ctx, "ws-1", 1, 20, now.Add(time.Hour)); ok {
		t.Error("the budget refilled after an hour")
	}
	// Even a minute before the window closes it is still spent, which is the
	// case a calendar reset got wrong.
	if ok, _ := l.Claim(ctx, "ws-1", 1, 20, now.Add(ca.UsageWindow-time.Minute)); ok {
		t.Error("the budget refilled before the window elapsed")
	}
	// Once the claim leaves the window there is room again.
	if ok, _ := l.Claim(ctx, "ws-1", 20, 20, now.Add(ca.UsageWindow+time.Hour)); !ok {
		t.Error("the oldest hour never aged out of the window")
	}
}

// Work that was claimed and never done is given back. Under the old counter a
// provider outage could exhaust the day having classified nothing.
func TestUsageLimiterReturnsUnusedBudget(t *testing.T) {
	l := NewUsageLimiter(newFakeState())
	ctx := context.Background()

	if ok, _ := l.Claim(ctx, "ws-1", 20, 20, now); !ok {
		t.Fatal("the claim must fit")
	}
	if ok, _ := l.Claim(ctx, "ws-1", 5, 20, now); ok {
		t.Fatal("the budget should be spent")
	}

	// The provider dropped the whole batch.
	if err := l.Release(ctx, "ws-1", 20, now); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if ok, _ := l.Claim(ctx, "ws-1", 20, 20, now); !ok {
		t.Error("budget for work that was never done was not returned")
	}
}

// What the dashboard reads, without spending any of it.
func TestUsageLimiterReadsWithoutClaiming(t *testing.T) {
	l := NewUsageLimiter(newFakeState())
	ctx := context.Background()
	if ok, err := l.Claim(ctx, "ws-1", 300, 20_000, now); !ok || err != nil {
		t.Fatalf("Claim: ok=%v err=%v", ok, err)
	}

	usage, err := l.Read(ctx, "ws-1", 20_000, now)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if usage.Used != 300 || usage.Limit != 20_000 || usage.Remaining() != 19_700 {
		t.Fatalf("usage = %+v", usage)
	}
	if usage.Exhausted() {
		t.Error("a budget with room left reported as exhausted")
	}
	// Reading again must not have moved anything.
	if again, _ := l.Read(ctx, "ws-1", 20_000, now); again.Used != 300 {
		t.Errorf("reading the budget changed it: %d", again.Used)
	}
	// And it says when room next frees up, which is when the oldest claim
	// leaves the window rather than a midnight that no longer applies.
	if want := now.UTC().Truncate(ca.UsageBucket).Add(ca.UsageWindow); !usage.FreesAt().Equal(want) {
		t.Errorf("FreesAt() = %v, want %v", usage.FreesAt(), want)
	}
}

// A refund larger than what is counted must not leave a negative reading on a
// dashboard. Nobody is owed budget.
func TestUsageLimiterNeverReportsNegativeUse(t *testing.T) {
	l := NewUsageLimiter(newFakeState())
	ctx := context.Background()
	if ok, err := l.Claim(ctx, "ws-1", 5, 20, now); !ok || err != nil {
		t.Fatalf("Claim: ok=%v err=%v", ok, err)
	}

	if err := l.Release(ctx, "ws-1", 50, now); err != nil {
		t.Fatal(err)
	}
	usage, _ := l.Read(ctx, "ws-1", 20, now)
	if usage.Used != 0 {
		t.Errorf("used = %d, want it floored at 0", usage.Used)
	}
}
