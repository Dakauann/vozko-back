package conversation

import (
	"errors"
	"fmt"
	"testing"
)

func TestOnlyATurnThatNeverStartedOrWasPacedIsRetried(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"paced by the reply rate limit", ReplySkipped{Reason: ReplySkipRateLimited}, true},
		{"a transient read before the turn", fmt.Errorf("read the lead: %w", errors.Join(ErrAgentTurnNotStarted, errors.New("conn reset"))), true},
		{"no balance", ReplySkipped{Reason: ReplySkipInsufficientBalance}, false},
		{"the model chose not to answer", ReplySkipped{Reason: ReplySkipEmptyReply}, false},
		{"window closed", ReplySkipped{Reason: ReplySkipWindowClosed}, false},
		{"withheld", ReplySkipped{Reason: ReplySkipWithheld}, false},
		{"opted out", ReplySkipped{Reason: "lead_opted_out"}, false},
		{"an unknown skip", ReplySkipped{Reason: "something_new"}, false},
		{"a failure after the model ran", errors.New("send text: status 500"), false},
		{"sent", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TurnRetryable(tc.err); got != tc.want {
				t.Fatalf("TurnRetryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestASkipKeepsItsReasonThroughWrapping(t *testing.T) {
	reason, ok := SkipReasonOf(fmt.Errorf("continue: %w", ReplySkipped{Reason: ReplySkipWithheld}))
	if !ok || reason != ReplySkipWithheld {
		t.Fatalf("reason = %q ok = %v", reason, ok)
	}
	if _, ok := SkipReasonOf(errors.New("boom")); ok {
		t.Fatal("a failure is not a skip")
	}
}
