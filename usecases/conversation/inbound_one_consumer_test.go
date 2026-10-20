package conversation_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/workflow"
)

type waitingRunEvaluator struct {
	recordingEvaluator
	mu        sync.Mutex
	waiting   *workflow.WorkflowRun
	lookupErr error
	lookups   int
	woken     []workflow.TriggerEvent
}

func (w *waitingRunEvaluator) Evaluate(e workflow.TriggerEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.recordingEvaluator.Evaluate(e)
}

func (w *waitingRunEvaluator) WaitingForReply(workflow.TriggerEvent) (*workflow.WorkflowRun, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lookups++
	return w.waiting, w.lookupErr
}

func (w *waitingRunEvaluator) WakeForReply(run *workflow.WorkflowRun, event workflow.TriggerEvent) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.woken = append(w.woken, event)
	return true
}

func (w *waitingRunEvaluator) counts() (woken, evaluated int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.woken), len(w.events)
}

func waitingRun() *workflow.WorkflowRun {
	return &workflow.WorkflowRun{ID: "run-9", WorkflowID: "wf-survey", EntryID: "e-1", Status: workflow.RunStatusWaiting, WaitReason: workflow.WaitReasonReply}
}

func agentOnly() conversation.ChannelAutomation {
	agent := "agent-1"
	return conversation.ChannelAutomation{AgentID: &agent, EnableAgentResponses: true}
}

func automationWith(evaluator *waitingRunEvaluator) (*InboundAutomation, *recordingReplier) {
	rep := &recordingReplier{}
	return NewInboundAutomation(evaluator, rep, &recordingScheduler{}, stubInboundCounter{count: 2}), rep
}

func TestARunWaitingForAReplyTakesTheMessageAndTheAgentStaysQuiet(t *testing.T) {
	evaluator := &waitingRunEvaluator{waiting: waitingRun()}
	auto, rep := automationWith(evaluator)
	in := automationInput()
	in.Config = agentOnly()

	if err := auto.Dispatch(context.Background(), in); err != nil {
		t.Fatal(err)
	}

	if len(evaluator.woken) != 1 || evaluator.woken[0].Data["message"] != "oi" {
		t.Fatalf("woken = %+v, want the waiting run woken with the message although the channel runs no workflows", evaluator.woken)
	}
	if len(rep.reqs) != 0 {
		t.Fatalf("the agent replied to a message the run consumed: %+v", rep.reqs)
	}
}

func TestWithoutAWaitingRunTheAgentReplies(t *testing.T) {
	evaluator := &waitingRunEvaluator{}
	auto, rep := automationWith(evaluator)
	in := automationInput()
	in.Config = agentOnly()

	_ = auto.Dispatch(context.Background(), in)

	if len(evaluator.woken) != 0 {
		t.Fatalf("woke %d runs with none waiting", len(evaluator.woken))
	}
	if len(rep.reqs) != 1 {
		t.Fatalf("agent requests = %d, want 1", len(rep.reqs))
	}
}

func TestAnUnreadableWaitingRunMeansNoAutomatedReplyAtAll(t *testing.T) {
	evaluator := &waitingRunEvaluator{lookupErr: errors.New("db down")}
	auto, rep := automationWith(evaluator)
	sch := &recordingScheduler{}
	auto.analysis = sch

	_ = auto.Dispatch(context.Background(), automationInput())

	if len(rep.reqs) != 0 || len(evaluator.woken) != 0 || len(evaluator.events) != 0 {
		t.Fatalf("agent = %d, woken = %d, evaluated = %d; a failed lookup must silence every automated reply",
			len(rep.reqs), len(evaluator.woken), len(evaluator.events))
	}
	if len(sch.scheduled) != 1 {
		t.Fatalf("analysis scheduled = %d, the rest of the inbound work must go on", len(sch.scheduled))
	}
}

func TestTheEvaluatorIsToldTheRunAlreadyTookTheMessage(t *testing.T) {
	evaluator := &waitingRunEvaluator{waiting: waitingRun()}
	auto, _ := automationWith(evaluator)

	_ = auto.Dispatch(context.Background(), automationInput())

	if evaluator.lookups != 1 {
		t.Fatalf("lookups = %d, want one per inbound message", evaluator.lookups)
	}
	if len(evaluator.events) == 0 {
		t.Fatal("other message-trigger workflows may still start")
	}
	routed := evaluator.events[0].Reply
	if routed == nil || routed.RunID != "run-9" || routed.WorkflowID != "wf-survey" {
		t.Fatalf("reply routing = %+v, want the woken run so the evaluator neither wakes nor restarts it", routed)
	}
}

func TestAPostbackStillWakesAWaitingRunWithoutTheAgent(t *testing.T) {
	evaluator := &waitingRunEvaluator{waiting: waitingRun()}
	auto, rep := automationWith(evaluator)
	in := automationInput()
	in.Config = agentOnly()

	auto.FireWorkflows(context.Background(), in)

	if len(evaluator.woken) != 1 || len(rep.reqs) != 0 {
		t.Fatalf("woken = %d, agent = %d", len(evaluator.woken), len(rep.reqs))
	}
}
