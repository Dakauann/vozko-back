package cdr

import "testing"

func TestTheStatusAtTheEndFollowsWhetherTheCallWasAnswered(t *testing.T) {
	cases := []struct {
		answered bool
		reason   string
		want     Status
	}{
		{true, "ended", StatusCompleted},
		{true, "insufficient_balance", StatusCompleted},
		{false, "busy", StatusFailed},
		{false, "no_answer", StatusFailed},
		{false, EndReasonCancelled, StatusAbandoned},
		{false, EndReasonInterrupted, StatusAbandoned},
	}
	for _, tc := range cases {
		if got := CompletionStatus(tc.answered, tc.reason); got != tc.want {
			t.Errorf("answered=%v reason=%q: status = %s, want %s", tc.answered, tc.reason, got, tc.want)
		}
	}
}
