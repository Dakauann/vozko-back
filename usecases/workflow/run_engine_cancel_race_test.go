package workflow_usecase

import (
	"errors"
	"sync"
	"testing"

	"vozko/domain/workflow"
)

type cancelOnStatusCheck struct {
	runs    *MockWorkflowRunRepository
	atNode  string
	current func() string
	checked []string
}

func (c *cancelOnStatusCheck) RunStatus(runID string) (workflow.RunStatus, error) {
	node := c.current()
	c.checked = append(c.checked, node)
	if node == c.atNode {
		if err := NewCancelRunUseCase(c.runs, c.runs, NewMockWorkflowRunLogRepository(), nil, seesEverything{}).Execute(viewer, "ws-1", runID); err != nil {
			return "", err
		}
	}
	return c.runs.RunStatus(runID)
}

type finishedSpy struct {
	mu     sync.Mutex
	events []workflow.AgentRunFinished
	err    error
}

func (s *finishedSpy) PublishAgentRunFinished(ev workflow.AgentRunFinished) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
	return s.err
}

func twoStepWorkflow(h *traceHarness, ranB *bool) *workflow.Workflow {
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		if ctx.Node.ID == "b" {
			*ranB = true
		}
		return &workflow.NodeResult{Output: map[string]interface{}{"ok": true}}, nil
	})
	return linearWorkflow(
		workflow.Node{ID: "a", Type: workflow.NodeTypeActionHTTPRequest},
		workflow.Node{ID: "b", Type: workflow.NodeTypeActionHTTPRequest},
	)
}

func TestACancelThatLandsBetweenStepsStopsTheRunAtTheNextBoundary(t *testing.T) {
	h := newTraceHarness()
	ranB := false
	w := twoStepWorkflow(h, &ranB)
	var live *workflow.WorkflowRun
	checks := &cancelOnStatusCheck{runs: h.runs, atNode: "b", current: func() string { return live.CurrentNodeID }}
	h.engine.SetRunStatusReader(checks)

	live = &workflow.WorkflowRun{
		ID: "run-1", WorkflowID: w.ID, WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp",
		Status: workflow.RunStatusRunning, CurrentNodeID: "t1", TriggerNodeID: "t1", State: workflow.NewRunState(),
	}
	_ = h.runs.Create(live)
	if err := h.engine.Execute(live, w); err != nil {
		t.Fatalf("an external cancel is a clean stop, got %v", err)
	}

	if ranB {
		t.Fatal("node b ran after the run was cancelled")
	}
	stored, _ := h.runs.FindByID("run-1")
	if stored.Status != workflow.RunStatusCancelled {
		t.Fatalf("stored status = %q, want the cancel to stick", stored.Status)
	}
	row := h.step(t, "b")
	if row.Status != workflow.LogStatusCancelled || row.ErrorKind != workflow.ErrorKindCancelled {
		t.Fatalf("step at b = %+v, want a cancelled row", row)
	}
}

func TestASaveAfterAnExternalCancelNeitherRevivesTheRunNorRunsTheNextNode(t *testing.T) {
	h := newTraceHarness()
	ranB := false
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		if ctx.Node.ID == "a" {
			if err := NewCancelRunUseCase(h.runs, h.runs, h.logs, nil, seesEverything{}).Execute(viewer, "ws-1", ctx.Run.ID); err != nil {
				t.Fatal(err)
			}
		}
		if ctx.Node.ID == "b" {
			ranB = true
		}
		return &workflow.NodeResult{Output: map[string]interface{}{"ok": true}}, nil
	})
	w := linearWorkflow(
		workflow.Node{ID: "a", Type: workflow.NodeTypeActionHTTPRequest},
		workflow.Node{ID: "b", Type: workflow.NodeTypeActionHTTPRequest},
	)

	run := h.start(w)

	if ranB {
		t.Fatal("node b ran although the run was cancelled while a was executing")
	}
	stored, _ := h.runs.FindByID(run.ID)
	if stored.Status != workflow.RunStatusCancelled {
		t.Fatalf("stored status = %q, the late save overwrote the cancel", stored.Status)
	}
	if row := h.step(t, "b"); row.Status != workflow.LogStatusCancelled {
		t.Fatalf("step at b = %+v, want a cancelled row where the run stopped", row)
	}
	if len(h.metrics.finished) != 0 {
		t.Fatalf("finished metric = %v, the engine did not finish this run", h.metrics.finished)
	}
}

func TestAnUnreadableStatusStopsTheRunWithoutTouchingIt(t *testing.T) {
	h := newTraceHarness()
	ranB := false
	w := twoStepWorkflow(h, &ranB)
	h.engine.SetRunStatusReader(failingStatus{})
	run := &workflow.WorkflowRun{
		ID: "run-1", WorkflowID: w.ID, WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp",
		Status: workflow.RunStatusRunning, CurrentNodeID: "t1", TriggerNodeID: "t1", State: workflow.NewRunState(),
	}
	_ = h.runs.Create(run)

	err := h.engine.Execute(run, w)
	if err == nil {
		t.Fatal("an unreadable status must stop the step and surface the error")
	}
	stored, _ := h.runs.FindByID("run-1")
	if stored.Status != workflow.RunStatusRunning || stored.CurrentNodeID != "t1" {
		t.Fatalf("stored run = %+v, it must stay as it was for the stuck-run sweep", stored)
	}
}

type failingStatus struct{}

func (failingStatus) RunStatus(string) (workflow.RunStatus, error) { return "", errors.New("db down") }

func agentStartedRun(w *workflow.Workflow, result map[string]interface{}) func(run *workflow.WorkflowRun) {
	return func(run *workflow.WorkflowRun) {
		run.StartedBy = workflow.StartedByAgent("agent-7")
		run.AgentResult = result
	}
}

func TestAnAgentStartedRunThatRepliedAnnouncesItsEndOnce(t *testing.T) {
	h := newTraceHarness()
	spy := &finishedSpy{}
	h.engine.SetAgentRunFinishedPublisher(spy)
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{Output: map[string]interface{}{}}, nil
	})
	w := linearWorkflow(workflow.Node{ID: "a", Type: workflow.NodeTypeActionHTTPRequest})

	h.startWith(w, agentStartedRun(w, map[string]interface{}{"status": "matriculado"}))

	if len(spy.events) != 1 {
		t.Fatalf("published %d events, want exactly 1", len(spy.events))
	}
	ev := spy.events[0]
	if ev.RunID != "run-1" || ev.AgentID != "agent-7" || ev.Continuation() != workflow.ContinuationResult || ev.EntryID != "entry-1" {
		t.Fatalf("event = %+v", ev)
	}
}

func TestAFailedAgentRunAnnouncesItsEnd(t *testing.T) {
	h := newTraceHarness()
	spy := &finishedSpy{}
	h.engine.SetAgentRunFinishedPublisher(spy)
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{Error: "HTTP 500"}, nil
	})
	w := linearWorkflow(workflow.Node{ID: "a", Type: workflow.NodeTypeActionHTTPRequest})

	h.startWith(w, agentStartedRun(w, nil))

	if len(spy.events) != 1 || spy.events[0].Continuation() != workflow.ContinuationFailure {
		t.Fatalf("events = %+v, want one failure", spy.events)
	}
}

func TestRunsThatOweTheAgentNothingAnnounceNothing(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(w *workflow.Workflow) func(run *workflow.WorkflowRun)
	}{
		{"a trigger run", func(*workflow.Workflow) func(run *workflow.WorkflowRun) { return nil }},
		{"an agent run that handled the conversation itself", func(w *workflow.Workflow) func(run *workflow.WorkflowRun) {
			return agentStartedRun(w, nil)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTraceHarness()
			spy := &finishedSpy{}
			h.engine.SetAgentRunFinishedPublisher(spy)
			h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
				return &workflow.NodeResult{Output: map[string]interface{}{}}, nil
			})
			w := linearWorkflow(workflow.Node{ID: "a", Type: workflow.NodeTypeActionHTTPRequest})
			h.startWith(w, tc.prepare(w))
			if len(spy.events) != 0 {
				t.Fatalf("published %+v", spy.events)
			}
		})
	}
}

func TestAPublishFailureNeverFailsTheRun(t *testing.T) {
	h := newTraceHarness()
	h.engine.SetAgentRunFinishedPublisher(&finishedSpy{err: errors.New("queue down")})
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{Output: map[string]interface{}{}}, nil
	})
	w := linearWorkflow(workflow.Node{ID: "a", Type: workflow.NodeTypeActionHTTPRequest})

	run := h.startWith(w, agentStartedRun(w, map[string]interface{}{"status": "ok"}))

	stored, _ := h.runs.FindByID(run.ID)
	if stored.Status != workflow.RunStatusCompleted {
		t.Fatalf("status = %q", stored.Status)
	}
}
