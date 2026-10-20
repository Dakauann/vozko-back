package workflow_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/workflow"
)

type backlogStub struct {
	runs         []*workflow.WorkflowRun
	from, before time.Time
	limit        int
}

func (b *backlogStub) FinishedWithoutContinuation(from, before time.Time, limit int) ([]*workflow.WorkflowRun, error) {
	b.from, b.before, b.limit = from, before, limit
	return b.runs, nil
}

type finishedEventSpy struct {
	events []workflow.AgentRunFinished
	failOn string
}

func (s *finishedEventSpy) PublishAgentRunFinished(ev workflow.AgentRunFinished) error {
	if ev.RunID == s.failOn {
		return errors.New("broker down")
	}
	s.events = append(s.events, ev)
	return nil
}

func agentRun(id string, status workflow.RunStatus, result map[string]interface{}) *workflow.WorkflowRun {
	return &workflow.WorkflowRun{
		ID: id, WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "e-1", EntryType: "instagram",
		StartedBy: workflow.StartedByAgent("agent-1"), Status: status, AgentResult: result,
	}
}

func TestTheSweepRequeuesOnlyRunsThatStillOweTheAgentATurn(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	backlog := &backlogStub{runs: []*workflow.WorkflowRun{
		agentRun("failed", workflow.RunStatusError, nil),
		agentRun("replied", workflow.RunStatusCompleted, map[string]interface{}{"ok": true}),
		agentRun("silent", workflow.RunStatusCompleted, nil),
		agentRun("cancelled", workflow.RunStatusCancelled, nil),
		{ID: "trigger", Status: workflow.RunStatusError, StartedBy: "trigger:trigger_webhook"},
	}}
	spy := &finishedEventSpy{failOn: "failed"}
	sweep := NewAgentContinuationSweep(backlog, spy)
	sweep.now = func() time.Time { return now }

	err := sweep.Execute(context.Background())

	if err == nil {
		t.Fatal("a publish failure must be reported so the next sweep is noticed")
	}
	if len(spy.events) != 1 || spy.events[0].RunID != "replied" || spy.events[0].Continuation() != workflow.ContinuationResult {
		t.Fatalf("events = %+v", spy.events)
	}
	if !backlog.from.Equal(now.Add(-workflow.ContinuationSweepWindow)) || !backlog.before.Equal(now.Add(-workflow.ContinuationSweepGrace)) || backlog.limit != workflow.ContinuationSweepLimit {
		t.Fatalf("window = %v to %v limit %d", backlog.from, backlog.before, backlog.limit)
	}
}
