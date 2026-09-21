package audience

import (
	"testing"
	"time"
)

func TestUsageWindowIsMeasuredFromNowNotFromMidnight(t *testing.T) {
	now := time.Date(2026, 9, 11, 23, 30, 0, 0, time.UTC)
	buckets := UsageBucketsInWindow(now)

	if len(buckets) != 24 {
		t.Fatalf("window holds %d buckets, want 24", len(buckets))
	}
	if buckets[0] != UsageBucketKey(now) {
		t.Errorf("newest bucket = %q, want the current hour %q", buckets[0], UsageBucketKey(now))
	}
	oldest := UsageBucketKey(now.Add(-23 * time.Hour))
	if buckets[len(buckets)-1] != oldest {
		t.Errorf("oldest bucket = %q, want %q from the previous day", buckets[len(buckets)-1], oldest)
	}

	next := UsageBucketsInWindow(now.Add(time.Hour))
	if next[0] == buckets[0] {
		t.Error("the window did not advance after an hour")
	}
	if hasBucket(next, buckets[len(buckets)-1]) {
		t.Error("the oldest hour did not age out of the window")
	}
	if !hasBucket(next, buckets[0]) {
		t.Error("the window dropped an hour that is still inside it")
	}
}

func hasBucket(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}

func TestUsageBucketKeyRoundTrips(t *testing.T) {
	at := time.Date(2026, 9, 11, 20, 47, 13, 0, time.UTC)

	parsed, err := ParseUsageBucketKey(UsageBucketKey(at))
	if err != nil {
		t.Fatalf("ParseUsageBucketKey: %v", err)
	}
	if want := at.Truncate(time.Hour); !parsed.Equal(want) {
		t.Errorf("round trip = %v, want the truncated hour %v", parsed, want)
	}
	if _, err := ParseUsageBucketKey("not-a-bucket"); err == nil {
		t.Error("a malformed bucket key was accepted")
	}
}

func TestUsageReportsRemainingAndExhaustion(t *testing.T) {
	oldest := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		usage     Usage
		remaining int
		exhausted bool
	}{
		{"room left", Usage{Used: 300, Limit: 20_000}, 19_700, false},
		{"exactly spent", Usage{Used: 20_000, Limit: 20_000}, 0, true},
		{
			"overshot", Usage{Used: 20_040, Limit: 20_000}, 0, true,
		},
		{"no limit configured", Usage{Used: 5_000, Limit: 0}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.usage.Remaining(); got != tc.remaining {
				t.Errorf("Remaining() = %d, want %d", got, tc.remaining)
			}
			if got := tc.usage.Exhausted(); got != tc.exhausted {
				t.Errorf("Exhausted() = %v, want %v", got, tc.exhausted)
			}
		})
	}

	u := Usage{Used: 20_000, Limit: 20_000, OldestAt: oldest}
	if want := oldest.Add(UsageWindow); !u.FreesAt().Equal(want) {
		t.Errorf("FreesAt() = %v, want %v", u.FreesAt(), want)
	}
	if !(Usage{Used: 0, Limit: 20_000}).FreesAt().IsZero() {
		t.Error("an empty budget reported a time to wait for")
	}
}

func TestUsageBacklogTellsTheOperatorTheCeilingIsTheConstraint(t *testing.T) {
	t.Run("a backlog inside what is left is not the ceiling's doing", func(t *testing.T) {
		u := Usage{Used: 100, Limit: 1000, Waiting: 40}
		if u.Constrained() {
			t.Error("40 waiting against 900 remaining blamed the ceiling")
		}
	})

	t.Run("a backlog larger than what is left is", func(t *testing.T) {
		u := Usage{Used: 900, Limit: 1000, Waiting: 400}
		if !u.Constrained() {
			t.Error("400 waiting against 100 remaining did not blame the ceiling")
		}
	})

	t.Run("an exhausted budget with work waiting is", func(t *testing.T) {
		u := Usage{Used: 1000, Limit: 1000, Waiting: 1}
		if !u.Constrained() {
			t.Error("an exhausted budget with work waiting did not blame the ceiling")
		}
	})

	t.Run("an exhausted budget with nothing waiting is not", func(t *testing.T) {
		u := Usage{Used: 1000, Limit: 1000}
		if u.Constrained() {
			t.Error("an idle exhausted budget was reported as constrained")
		}
	})

	t.Run("no ceiling can never be the constraint", func(t *testing.T) {
		u := Usage{Used: 5000, Limit: 0, Waiting: 5000}
		if u.Constrained() {
			t.Error("an unlimited workspace blamed its ceiling")
		}
	})
}

func TestUsageClearsInEstimatesTheWait(t *testing.T) {
	t.Run("what fits in what is left clears on the next pass", func(t *testing.T) {
		u := Usage{Used: 100, Limit: 1000, Waiting: 40}
		if got := u.ClearsIn(); got != 0 {
			t.Errorf("ClearsIn() = %v, want 0 for a backlog that fits", got)
		}
	})

	t.Run("the overflow waits for the window to free that much room", func(t *testing.T) {
		u := Usage{Used: 900, Limit: 1000, Waiting: 1300}
		got := u.ClearsIn()
		if want := time.Duration(1.2 * float64(UsageWindow)); got != want {
			t.Errorf("ClearsIn() = %v, want %v", got, want)
		}
	})

	t.Run("no ceiling means no wait to report", func(t *testing.T) {
		u := Usage{Limit: 0, Waiting: 5000}
		if got := u.ClearsIn(); got != 0 {
			t.Errorf("ClearsIn() = %v, want 0 without a ceiling", got)
		}
	})
}

func TestResolveDailyCap(t *testing.T) {
	t.Run("the workspace ceiling wins when one is set", func(t *testing.T) {
		if got := ResolveDailyCap(250, 20000); got != 250 {
			t.Errorf("ResolveDailyCap(250, 20000) = %d, want 250", got)
		}
	})

	t.Run("an unset workspace ceiling falls back to the account's", func(t *testing.T) {
		if got := ResolveDailyCap(0, 500); got != 500 {
			t.Errorf("ResolveDailyCap(0, 500) = %d, want the account cap 500", got)
		}
	})

	t.Run("neither set falls back to the default", func(t *testing.T) {
		if got := ResolveDailyCap(0, 0); got != DefaultDailyCap {
			t.Errorf("ResolveDailyCap(0, 0) = %d, want %d", got, DefaultDailyCap)
		}
	})

	t.Run("a negative stored value is treated as unset", func(t *testing.T) {
		if got := ResolveDailyCap(-5, 300); got != 300 {
			t.Errorf("ResolveDailyCap(-5, 300) = %d, want 300", got)
		}
	})
}
