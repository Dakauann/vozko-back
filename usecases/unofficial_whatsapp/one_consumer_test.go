package unofficial_whatsapp

import (
	"errors"
	"sync"
	"testing"

	"vozko/domain/workflow"
	conversation_usecase "vozko/usecases/conversation"
)

type waitingRuns struct {
	mu        sync.Mutex
	waiting   *workflow.WorkflowRun
	lookupErr error
	woken     []workflow.TriggerEvent
}

func (w *waitingRuns) WaitingForReply(workflow.TriggerEvent) (*workflow.WorkflowRun, error) {
	return w.waiting, w.lookupErr
}

func (w *waitingRuns) WakeForReply(_ *workflow.WorkflowRun, event workflow.TriggerEvent) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.woken = append(w.woken, event)
	return true
}

func (w *waitingRuns) wakes() []workflow.TriggerEvent {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]workflow.TriggerEvent(nil), w.woken...)
}

func harnessWithWaiters(t *testing.T, waiters *waitingRuns) *autoHarness {
	t.Helper()
	h := newAutoHarness(t, nil)
	h.uc.router = conversation_usecase.NewInboundRouter(waiters)
	return h
}

func TestAWaitingRunGetsTheUnofficialMessageAndTheAgentStaysQuiet(t *testing.T) {
	waiters := &waitingRuns{waiting: &workflow.WorkflowRun{ID: "run-9", WorkflowID: "wf-survey", Status: workflow.RunStatusWaiting, WaitReason: workflow.WaitReasonReply}}
	h := harnessWithWaiters(t, waiters)

	h.deliverInbound(t)

	if woken := waiters.wakes(); len(woken) != 1 || woken[0].Data[workflow.DataKeyMessage] != "oi" {
		t.Fatalf("woken = %+v, want the waiting run to receive the message", woken)
	}
	if reqs := h.ai.all(); len(reqs) != 0 {
		t.Fatalf("the agent answered a message the waiting run consumed: %+v", reqs)
	}
}

func TestAnUnreadableWaitingRunSilencesBothTheRunAndTheAgentOnUnofficialWhatsApp(t *testing.T) {
	waiters := &waitingRuns{lookupErr: errors.New("db down")}
	h := harnessWithWaiters(t, waiters)

	h.deliverInbound(t)

	if len(waiters.wakes()) != 0 || len(h.ai.all()) != 0 || len(h.workflows.all()) != 0 {
		t.Fatalf("woken = %d, agent = %d, evaluated = %d; a failed lookup must start, wake and answer nothing",
			len(waiters.wakes()), len(h.ai.all()), len(h.workflows.all()))
	}
}

func TestWithoutAWaitingRunTheUnofficialAgentReplies(t *testing.T) {
	waiters := &waitingRuns{}
	h := harnessWithWaiters(t, waiters)

	h.deliverInbound(t)

	if len(waiters.wakes()) != 0 || len(h.ai.all()) != 1 {
		t.Fatalf("woken = %d, agent = %d", len(waiters.wakes()), len(h.ai.all()))
	}
}
