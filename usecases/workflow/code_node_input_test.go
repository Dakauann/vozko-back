package workflow_usecase

import (
	"testing"

	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

type loginResponseExecutor struct{}

func (loginResponseExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{Type: workflow.NodeTypeActionHTTPRequest}
}

func (loginResponseExecutor) Execute(*workflow.NodeContext) (*workflow.NodeResult, error) {
	return &workflow.NodeResult{Output: map[string]interface{}{
		"status_code": 200,
		"body":        map[string]interface{}{"accessToken": "x"},
	}}, nil
}

func loginThenCodeWorkflow(code string) *workflow.Workflow {
	return &workflow.Workflow{
		ID:          "wf-1",
		WorkspaceID: "ws-1",
		Type:        workflow.WorkflowTypeMessages,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "n1", Type: workflow.NodeTypeTriggerWebhook},
				{ID: "n55", Type: workflow.NodeTypeActionHTTPRequest},
				{ID: "n56", Type: workflow.NodeTypeActionCode, Config: map[string]interface{}{"code": code}},
				{ID: "end", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{
				{Source: "n1", Target: "n55"},
				{Source: "n55", Target: "n56"},
				{Source: "n56", Target: "end"},
			},
		},
	}
}

func runLoginThenCode(t *testing.T, code string) *workflow.WorkflowRun {
	t.Helper()
	registry := NewNodeExecutorRegistry()
	registry.Register(workflow.NodeTypeActionHTTPRequest, loginResponseExecutor{})
	registry.Register(workflow.NodeTypeActionCode, node_executors.NewCodeExecutor(nil))
	w := loginThenCodeWorkflow(code)
	run := &workflow.WorkflowRun{
		ID:            "run-1",
		WorkflowID:    w.ID,
		WorkspaceID:   w.WorkspaceID,
		Status:        workflow.RunStatusRunning,
		CurrentNodeID: "n1",
		State:         workflow.NewRunState(),
	}
	runs := NewMockWorkflowRunRepository()
	_ = runs.Create(run)
	if err := NewRunEngine(runs, NewMockWorkflowRunLogRepository(), registry).Execute(run, w); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if run.Status != workflow.RunStatusCompleted {
		t.Fatalf("status = %q (%s), want completed", run.Status, run.Error)
	}
	return run
}

func TestCodeNodeReceivesThePreviousNodeOutputAsInputSoALoginStatusIsNotLost(t *testing.T) {
	cases := []struct {
		name string
		code string
	}{
		{"input field", `return { status: input.status_code, token: input.body.accessToken };`},
		{"node path through state", `return { status: state.get('node.n55.status_code'), token: state.get('node.n55.body.accessToken') };`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run := runLoginThenCode(t, c.code)
			status, _ := run.State.Get("_node_n56_status")
			token, _ := run.State.Get("_node_n56_token")
			if asNumber(status) != 200 || token != "x" {
				t.Fatalf("code node saw status=%v token=%v, want 200 and x", status, token)
			}
		})
	}
}

func asNumber(v interface{}) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	}
	return 0
}
