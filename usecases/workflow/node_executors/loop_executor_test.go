package node_executors

import (
	"testing"

	"vozko/domain/workflow"
)

func buildLoopGraph() *workflow.Graph {
	return &workflow.Graph{
		Nodes: []workflow.Node{
			{ID: "loop1", Type: workflow.NodeTypeActionLoop},
			{ID: "cond1", Type: workflow.NodeTypeConditionBranch},
			{ID: "send1", Type: workflow.NodeTypeActionSendText},
			{ID: "wait1", Type: workflow.NodeTypeWaitSchedule},
			{ID: "http1", Type: workflow.NodeTypeActionHTTPRequest},
			{ID: "end1", Type: workflow.NodeTypeEnd},
		},
		Edges: []workflow.Edge{
			{Source: "loop1", Target: "cond1", Label: "body"},
			{Source: "loop1", Target: "end1", Label: "done"},
			{Source: "cond1", Target: "loop1", Label: "true"},
			{Source: "cond1", Target: "send1", Label: "false"},
			{Source: "send1", Target: "wait1"},
			{Source: "wait1", Target: "http1", Label: "message_received"},
			{Source: "wait1", Target: "end1", Label: "completed"},
		},
	}
}

func TestIsInLoopBody_BlocksTraversalPastWaitNode(t *testing.T) {
	g := buildLoopGraph()

	if !isInLoopBody(g, "loop1", "send1") {
		t.Fatal("expected send1 to be considered inside loop body")
	}
	if !isInLoopBody(g, "loop1", "wait1") {
		t.Fatal("expected wait1 to be considered inside loop body")
	}
	if isInLoopBody(g, "loop1", "http1") {
		t.Fatal("expected http1 to be outside loop body because wait node is a traversal barrier")
	}
}

func TestLoopExecute_ResetsCounterWhenReEnteringFromOutsideBody(t *testing.T) {
	exec := NewLoopExecutor()
	g := buildLoopGraph()

	node := &workflow.Node{
		ID:   "loop1",
		Type: workflow.NodeTypeActionLoop,
		Config: map[string]interface{}{
			"list_variable": "items",
			"item_variable": "item",
		},
	}
	state := workflow.NewRunState()
	state.Set("items", []interface{}{"a", "b"})
	state.Set("_loop_loop1_index", float64(1))
	state.Set("_prev_node_id", "http1")

	ctx := &workflow.NodeContext{
		Node:  node,
		Graph: g,
		State: &state,
	}

	result, err := exec.Execute(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextNodeID != "cond1" {
		t.Fatalf("expected body target cond1, got %s", result.NextNodeID)
	}
	if got := result.Output["index"]; got != 0 {
		t.Fatalf("expected index reset to 0, got %v", got)
	}
	if got := state.GetString("item"); got != "a" {
		t.Fatalf("expected item to restart from first element, got %q", got)
	}
}

func TestToSlice_AcceptsTypedSlices(t *testing.T) {
	items := []string{"x", "y"}
	out := toSlice(items)
	if len(out) != 2 {
		t.Fatalf("expected len=2, got %d", len(out))
	}
	if out[0] != "x" || out[1] != "y" {
		t.Fatalf("unexpected values: %#v", out)
	}
}
