package conversation

import "testing"

func TestPendingOutcomeSkipsProgressAndFindsTheOutcome(t *testing.T) {
	events := make(chan CallEvent, 4)
	events <- CallEvent{Type: CallEventRinging}
	events <- CallEvent{Type: CallEventBusy, Reason: "486"}
	outcome, ok := PendingOutcome(events)
	if !ok || outcome.Type != CallEventBusy || outcome.Reason != "486" {
		t.Fatalf("PendingOutcome = %+v, %v", outcome, ok)
	}
}

func TestPendingOutcomeIsAbsentWhenNothingIsWaiting(t *testing.T) {
	events := make(chan CallEvent, 1)
	if _, ok := PendingOutcome(events); ok {
		t.Fatal("an empty channel reported an outcome")
	}
	close(events)
	if _, ok := PendingOutcome(events); ok {
		t.Fatal("a closed channel reported an outcome")
	}
}
