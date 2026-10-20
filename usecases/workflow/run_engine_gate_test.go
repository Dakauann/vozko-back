package workflow_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/workflow"
)

type gateFunc func(run *workflow.WorkflowRun, node *workflow.Node) (*workflow.GatedStep, error)

func (f gateFunc) BeforeStep(run *workflow.WorkflowRun, node *workflow.Node) (*workflow.GatedStep, error) {
	return f(run, node)
}

func pinTimeForTests() time.Time {
	return time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
}

func pinnedHTTPWorkflow() *workflow.Workflow {
	w := &workflow.Workflow{ID: "wf-1", WorkspaceID: "ws-1", Type: workflow.WorkflowTypeMessages}
	w.Graph = workflow.Graph{
		Nodes: []workflow.Node{
			{ID: "t1", Type: workflow.NodeTypeTriggerWebhook},
			{ID: "n55", Type: workflow.NodeTypeActionHTTPRequest, Pinned: &workflow.NodePin{
				Output: map[string]interface{}{"status_code": 500.0}, Edge: "error", Source: workflow.PinSourceManual, PinnedAt: time.Now().UTC(),
			}},
			{ID: "ok", Type: workflow.NodeTypeActionSetVariable},
			{ID: "fail", Type: workflow.NodeTypeActionSetVariable},
			{ID: "end", Type: workflow.NodeTypeEnd},
		},
		Edges: []workflow.Edge{
			{Source: "t1", Target: "n55"},
			{Source: "n55", Target: "ok", Label: "success"},
			{Source: "n55", Target: "fail", Label: "error"},
			{Source: "ok", Target: "end"},
			{Source: "fail", Target: "end"},
		},
	}
	return w
}

func (h *traceHarness) registerSetVariable() {
	h.register(workflow.NodeTypeActionSetVariable, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{Output: map[string]interface{}{}}, nil
	})
}

func TestTheProductionEngineIgnoresAPinThatReachesItAndExecutesTheNode(t *testing.T) {
	h := newTraceHarness()
	calls := 0
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		calls++
		return &workflow.NodeResult{Output: map[string]interface{}{"status_code": 200.0}, NextNodeID: "ok"}, nil
	})
	h.registerSetVariable()
	run := h.start(pinnedHTTPWorkflow())

	if calls != 1 {
		t.Fatalf("executor ran %d times, want 1: production never reads pins", calls)
	}
	step := h.step(t, "n55")
	if step.Pinned || step.Output["status_code"] != 200.0 {
		t.Fatalf("step = %+v, want the real output and no pin mark", step)
	}
	if run.Status != workflow.RunStatusCompleted {
		t.Fatalf("status = %s", run.Status)
	}
	h.step(t, "ok")
}

func TestAGateServingAPinnedResultReplacesTheExecutorAndMarksTheStep(t *testing.T) {
	h := newTraceHarness()
	calls := 0
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		calls++
		return &workflow.NodeResult{}, nil
	})
	h.registerSetVariable()
	w := pinnedHTTPWorkflow()
	h.engine.SetStepGate(gateFunc(func(_ *workflow.WorkflowRun, node *workflow.Node) (*workflow.GatedStep, error) {
		if node.Pinned == nil {
			return nil, nil
		}
		return &workflow.GatedStep{Result: workflow.PinnedResult(&w.Graph, node, node.Pinned), Pinned: true}, nil
	}))
	run := h.start(w)

	if calls != 0 {
		t.Fatal("a pinned node must not execute in a simulation")
	}
	step := h.step(t, "n55")
	if !step.Pinned || step.Branch != "error" || step.Output["status_code"] != 500.0 {
		t.Fatalf("step = %+v, want the pinned output, the pinned branch and the pin mark", step)
	}
	h.step(t, "fail")
	if run.Status != workflow.RunStatusCompleted {
		t.Fatalf("status = %s", run.Status)
	}
}

func TestAGateErrorStopsTheRunAsCancelledBeforeTheNodeRuns(t *testing.T) {
	h := newTraceHarness()
	calls := 0
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		calls++
		return &workflow.NodeResult{}, nil
	})
	h.registerSetVariable()
	stop := errors.New("paused simulation abandoned")
	h.engine.SetStepGate(gateFunc(func(_ *workflow.WorkflowRun, node *workflow.Node) (*workflow.GatedStep, error) {
		if node.ID == "n55" {
			return nil, stop
		}
		return nil, nil
	}))
	run := h.start(pinnedHTTPWorkflow())

	if calls != 0 {
		t.Fatal("the node behind the gate must not run")
	}
	if run.Status != workflow.RunStatusCancelled {
		t.Fatalf("status = %s, want cancelled", run.Status)
	}
	step := h.step(t, "n55")
	if step.Status != workflow.LogStatusCancelled || step.ErrorKind != workflow.ErrorKindCancelled {
		t.Fatalf("step = %+v", step)
	}
}

func TestAGateRefusalFailsTheNodeClosedWithItsKind(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		t.Fatal("a refused real call must not run")
		return nil, nil
	})
	h.registerSetVariable()
	h.engine.SetStepGate(gateFunc(func(_ *workflow.WorkflowRun, node *workflow.Node) (*workflow.GatedStep, error) {
		if node.ID != "n55" {
			return nil, nil
		}
		return &workflow.GatedStep{Result: &workflow.NodeResult{Error: "not confirmed", ErrorKind: workflow.ErrorKindRealCallNotConfirmed}}, nil
	}))
	run := h.start(pinnedHTTPWorkflow())

	if run.Status != workflow.RunStatusError {
		t.Fatalf("status = %s, want error", run.Status)
	}
	step := h.step(t, "n55")
	if step.ErrorKind != workflow.ErrorKindRealCallNotConfirmed || step.Pinned {
		t.Fatalf("step = %+v", step)
	}
}

func TestAGateIsConsultedBeforeEveryNodeIncludingTheTrigger(t *testing.T) {
	h := newTraceHarness()
	h.registerSetVariable()
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{NextNodeID: "ok"}, nil
	})
	var seen []string
	h.engine.SetStepGate(gateFunc(func(_ *workflow.WorkflowRun, node *workflow.Node) (*workflow.GatedStep, error) {
		seen = append(seen, node.ID)
		return nil, nil
	}))
	h.start(pinnedHTTPWorkflow())
	want := []string{"t1", "n55", "ok", "end"}
	if len(seen) != len(want) {
		t.Fatalf("gate saw %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("gate saw %v, want %v", seen, want)
		}
	}
}
