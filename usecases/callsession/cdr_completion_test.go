package callsession_usecase

import (
	"context"
	"sync"
	"testing"
	"time"

	"vozko/domain/calls/cdr"
	"vozko/domain/callsession"
	"vozko/domain/conversation"
)

type completedCalls struct {
	mu    sync.Mutex
	calls []cdr.CompleteCallInput
}

func (c *completedCalls) Execute(input cdr.CompleteCallInput) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, input)
	return nil
}

func (c *completedCalls) only(t *testing.T) cdr.CompleteCallInput {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.calls) != 1 {
		t.Fatalf("completed %d times, want once: %+v", len(c.calls), c.calls)
	}
	return c.calls[0]
}

func runToTheEnd(t *testing.T, ctx context.Context, call *fakeCRMCall, events ...conversation.CallEvent) *completedCalls {
	t.Helper()
	completed := &completedCalls{}
	runner := newRunner(&fakeAdmission{}, newFakeReserver(), &fakeBalanceChecker{balance: 1_000_000}, &fakeBillingPub{})
	runner.SetCDRComplete(completed)
	for _, event := range events {
		call.events <- event
	}
	done := make(chan struct{})
	go func() {
		runner.Run(ctx, OutboundCallLifecycleInput{
			Call: call, WorkspaceID: "ws-1", StartedAt: time.Now(),
			Admission: &callsession.CallAdmissionLease{WorkspaceID: "ws-1", ReservedMicros: 100, PerMinuteCostMicros: 100},
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the lifecycle never finished")
	}
	return completed
}

func TestAnAnsweredCallIsCompletedWithItsReason(t *testing.T) {
	call := newFakeCall("sip-out-1")
	completed := runToTheEnd(t, context.Background(), call,
		conversation.CallEvent{Type: conversation.CallEventAnswered},
		conversation.CallEvent{Type: conversation.CallEventEnded},
	)
	got := completed.only(t)
	if got.CallID != "sip-out-1" || got.Status != cdr.StatusCompleted || got.EndReason == nil || *got.EndReason != "ended" || got.EndedAt.IsZero() {
		t.Fatalf("completion = %+v", got)
	}
}

func TestAnUnansweredCallIsRecordedAsFailedWithWhatHappened(t *testing.T) {
	call := newFakeCall("sip-out-2")
	got := runToTheEnd(t, context.Background(), call, conversation.CallEvent{Type: conversation.CallEventBusy}).only(t)
	if got.Status != cdr.StatusFailed || *got.EndReason != "busy" {
		t.Fatalf("completion = %+v", got)
	}
}

func TestACallCancelledBeforeTheAnswerIsAbandoned(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := runToTheEnd(t, ctx, newFakeCall("sip-out-3")).only(t)
	if got.Status != cdr.StatusAbandoned || *got.EndReason != cdr.EndReasonCancelled {
		t.Fatalf("completion = %+v", got)
	}
}
