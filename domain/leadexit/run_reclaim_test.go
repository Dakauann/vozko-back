package leadexit

import (
	"testing"
	"time"
)

func TestWhichRunsAnInboundRetryMayTakeAgain(t *testing.T) {
	claimed := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		outcome Outcome
		after   time.Duration
		want    bool
	}{
		{OutcomeFailed, 0, true},
		{OutcomeRunning, 0, false},
		{OutcomeRunning, RunTimeout - time.Second, false},
		{OutcomeRunning, RunTimeout, true},
		{OutcomeChanged, time.Hour, false},
		{OutcomeUnchanged, time.Hour, false},
		{OutcomeRefused, time.Hour, false},
	}
	for _, tc := range cases {
		if got := Reclaimable(tc.outcome, claimed, claimed.Add(tc.after)); got != tc.want {
			t.Fatalf("%s after %v: got %v, want %v", tc.outcome, tc.after, got, tc.want)
		}
	}
}
