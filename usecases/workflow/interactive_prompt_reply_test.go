package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workflow"
)

// askGraph builds a workflow with a single interactive prompt node "ask" wired to
// the given edges, plus a run parked at "ask".
func askGraph(edges []workflow.Edge) (*workflow.WorkflowRun, *workflow.Workflow) {
	nodes := []workflow.Node{
		{ID: "ask", Type: workflow.NodeTypeActionSendInteractive},
		{ID: "a", Type: workflow.NodeTypeEnd},
		{ID: "b", Type: workflow.NodeTypeEnd},
		{ID: "nomatch", Type: workflow.NodeTypeEnd},
	}
	w := &workflow.Workflow{ID: "w1", Graph: workflow.Graph{Nodes: nodes, Edges: edges}}
	state := workflow.NewRunState()
	run := &workflow.WorkflowRun{
		ID:            "r1",
		WorkflowID:    "w1",
		CurrentNodeID: "ask",
		Status:        workflow.RunStatusWaiting,
		WaitReason:    workflow.WaitReasonReply,
		State:         state,
	}
	return run, w
}

func TestAdvanceOnReply_Interactive_RoutesBySelectedOption(t *testing.T) {
	run, w := askGraph([]workflow.Edge{
		{Source: "ask", Target: "a", Label: "sim"},
		{Source: "ask", Target: "b", Label: "nao"},
	})

	err := AdvanceOnReply(run, w, map[string]interface{}{"selected_option_id": "nao"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.CurrentNodeID != "b" {
		t.Fatalf("expected route to 'b' (option nao), got %q", run.CurrentNodeID)
	}
	if got := run.State.GetString("_wait_outcome"); got != "nao" {
		t.Fatalf("expected _wait_outcome 'nao', got %q", got)
	}
	if run.Status != workflow.RunStatusRunning {
		t.Fatalf("expected run running, got %s", run.Status)
	}
}

func TestAdvanceOnReply_Interactive_NoMatchFallback(t *testing.T) {
	run, w := askGraph([]workflow.Edge{
		{Source: "ask", Target: "a", Label: "sim"},
		{Source: "ask", Target: "nomatch", Label: "no_match"},
	})

	err := AdvanceOnReply(run, w, map[string]interface{}{"selected_option_id": "unknown_option"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.CurrentNodeID != "nomatch" {
		t.Fatalf("expected route to 'nomatch', got %q", run.CurrentNodeID)
	}
	if got := run.State.GetString("_wait_outcome"); got != "no_match" {
		t.Fatalf("expected _wait_outcome 'no_match', got %q", got)
	}
}

func TestAdvanceOnReply_Interactive_LegacyDefaultEdge(t *testing.T) {
	// A pre-branching send_whatsapp_button node: a single unlabeled edge.
	run, w := askGraph([]workflow.Edge{
		{Source: "ask", Target: "a", Label: ""},
	})

	err := AdvanceOnReply(run, w, map[string]interface{}{"selected_option_id": "whatever"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.CurrentNodeID != "a" {
		t.Fatalf("expected legacy default route to 'a', got %q", run.CurrentNodeID)
	}
}

func TestAdvanceOnReply_Interactive_UnhandledLeavesRunParked(t *testing.T) {
	// Only specific option edges, no no_match, no default: a stray reply is
	// unhandled and the run must NOT advance (caller leaves it parked).
	run, w := askGraph([]workflow.Edge{
		{Source: "ask", Target: "a", Label: "sim"},
		{Source: "ask", Target: "b", Label: "nao"},
	})

	err := AdvanceOnReply(run, w, map[string]interface{}{"selected_option_id": "typed_free_text"})
	if !errors.Is(err, ErrInteractiveReplyUnhandled) {
		t.Fatalf("expected ErrInteractiveReplyUnhandled, got %v", err)
	}
	if run.CurrentNodeID != "ask" {
		t.Fatalf("expected run to stay parked at 'ask', got %q", run.CurrentNodeID)
	}
}

func TestAdvanceOnReply_WaitForReply_StillRoutesReplied(t *testing.T) {
	// Regression: the shared function must still route a plain wait_for_reply node.
	nodes := []workflow.Node{
		{ID: "wait", Type: workflow.NodeTypeWaitForReply},
		{ID: "next", Type: workflow.NodeTypeEnd},
	}
	w := &workflow.Workflow{ID: "w1", Graph: workflow.Graph{
		Nodes: nodes,
		Edges: []workflow.Edge{{Source: "wait", Target: "next", Label: "replied"}},
	}}
	state := workflow.NewRunState()
	run := &workflow.WorkflowRun{ID: "r1", WorkflowID: "w1", CurrentNodeID: "wait", State: state}

	if err := AdvanceOnReply(run, w, map[string]interface{}{"message": "oi"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.CurrentNodeID != "next" {
		t.Fatalf("expected route to 'next', got %q", run.CurrentNodeID)
	}
	if got := run.State.GetString("_wait_outcome"); got != "replied" {
		t.Fatalf("expected _wait_outcome 'replied', got %q", got)
	}
}
