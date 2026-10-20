package campaign

import (
	"testing"
	"time"
)

func TestOnlyARunningCampaignAsksToBeWatchedClosely(t *testing.T) {
	cases := []struct {
		status  Status
		refresh time.Duration
		ttl     time.Duration
	}{
		{StatusRunning, 3 * time.Second, 2 * time.Second},
		{StatusPaused, 0, 30 * time.Second},
		{StatusStopped, 0, 30 * time.Second},
		{StatusCompleted, 0, 30 * time.Second},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			if got := ProgressRefresh(tc.status); got != tc.refresh {
				t.Fatalf("refresh = %v, want %v", got, tc.refresh)
			}
			if got := ProgressFreshness(tc.status); got != tc.ttl {
				t.Fatalf("freshness = %v, want %v", got, tc.ttl)
			}
		})
	}
}

func TestPaceComesFromTheBaselineSample(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	counts := &Counts{Total: 1000, Pending: 600, Sent: 300, Delivered: 50, Failed: 50}
	cases := []struct {
		name     string
		status   Status
		baseline *ProgressSample
		want     *Pace
	}{
		{name: "no baseline yet", status: StatusRunning},
		{name: "a baseline too recent to trust", status: StatusRunning, baseline: &ProgressSample{Processed: 390, At: at.Add(-10 * time.Second)}},
		{name: "a minute of sending", status: StatusRunning, baseline: &ProgressSample{Processed: 280, At: at.Add(-time.Minute)}, want: &Pace{ProcessedPerMinute: 120, RemainingSeconds: secondsLeft(300)}},
		{name: "nothing moved", status: StatusRunning, baseline: &ProgressSample{Processed: 400, At: at.Add(-time.Minute)}, want: &Pace{ProcessedPerMinute: 0}},
		{name: "a paused campaign has no pace", status: StatusPaused, baseline: &ProgressSample{Processed: 280, At: at.Add(-time.Minute)}},
		{name: "a reset makes the baseline useless", status: StatusRunning, baseline: &ProgressSample{Processed: 900, At: at.Add(-time.Minute)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewProgress(tc.status, counts, at, tc.baseline)
			if got.Metrics.Processed != 400 || got.SampledAt != at || got.Status != tc.status {
				t.Fatalf("progress = %+v", got)
			}
			if !samePace(got.Pace, tc.want) {
				t.Fatalf("pace = %+v, want %+v", got.Pace, tc.want)
			}
		})
	}
}

func TestTheBaselineRollsForwardOnlyWhenItGetsOld(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	current := ProgressSample{Processed: 400, At: at}
	if got := NextProgressBaseline(nil, current); got != current {
		t.Fatalf("first sample = %+v", got)
	}
	recent := &ProgressSample{Processed: 300, At: at.Add(-time.Minute)}
	if got := NextProgressBaseline(recent, current); got != *recent {
		t.Fatalf("a minute old baseline must stay, got %+v", got)
	}
	old := &ProgressSample{Processed: 100, At: at.Add(-3 * time.Minute)}
	if got := NextProgressBaseline(old, current); got != current {
		t.Fatalf("a stale baseline must roll forward, got %+v", got)
	}
	reset := &ProgressSample{Processed: 900, At: at.Add(-time.Minute)}
	if got := NextProgressBaseline(reset, current); got != current {
		t.Fatalf("a baseline ahead of the counts must roll forward, got %+v", got)
	}
}

func secondsLeft(v int64) *int64 { return &v }

func samePace(a, b *Pace) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.ProcessedPerMinute != b.ProcessedPerMinute {
		return false
	}
	if a.RemainingSeconds == nil || b.RemainingSeconds == nil {
		return a.RemainingSeconds == nil && b.RemainingSeconds == nil
	}
	return *a.RemainingSeconds == *b.RemainingSeconds
}
