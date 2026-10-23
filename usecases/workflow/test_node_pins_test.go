package workflow_usecase

import (
	"context"
	"testing"

	"vozko/domain/workflow"
)

func pinnedLoginWorkflow() *workflow.Workflow {
	pin := &workflow.NodePin{
		Output: map[string]interface{}{"status_code": 200.0, "json": map[string]interface{}{"token": "PINNED", "aluno": "Ana"}},
		Edge:   "sucesso", Source: workflow.PinSourceManual, PinnedAt: pinTimeForTests(),
	}
	return createTestWorkflow("wf1", "ws1", []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerMessageReceived},
		{ID: "login", Type: workflow.NodeTypeActionHTTPRequest, Config: map[string]interface{}{"url": "https://login.example.com", "capture_variable": "login"}, Pinned: pin},
		{ID: "matricula", Type: workflow.NodeTypeActionHTTPRequest, Config: map[string]interface{}{"url": "https://api.example.com", "auth_token": "{{login.token}}"}},
	}, []workflow.Edge{
		{Source: "trigger1", Target: "login"},
		{Source: "login", Target: "matricula", Label: "sucesso"},
	})
}

func TestTheNodeTestUsesAnUpstreamPinInsteadOfCallingIt(t *testing.T) {
	repo := NewMockWorkflowRepository()
	_ = repo.Create(pinnedLoginWorkflow())
	ran := 0
	httpExec := &captureHTTPExecutor{ran: &ran}
	registry := NewNodeExecutorRegistry()
	registry.Register(workflow.NodeTypeActionHTTPRequest, httpExec)
	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Steps: registry, Executors: fixedExecutors(registry)})

	result, err := uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "matricula", ConfirmRealCalls: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || ran != 1 {
		t.Fatalf("success = %v (%s), executor ran %d times, want only the target to run", result.Success, result.Error, ran)
	}
	if httpExec.lastConfig["auth_token"] != "PINNED" {
		t.Fatalf("auth_token = %v, want the value from the pinned login", httpExec.lastConfig["auth_token"])
	}
}

func TestTestingAPinnedNodeReturnsItsPin(t *testing.T) {
	repo := NewMockWorkflowRepository()
	_ = repo.Create(pinnedLoginWorkflow())
	ran := 0
	registry := NewNodeExecutorRegistry()
	registry.Register(workflow.NodeTypeActionHTTPRequest, &captureHTTPExecutor{ran: &ran})
	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Steps: registry, Executors: fixedExecutors(registry)})

	result, err := uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "login"})
	if err != nil {
		t.Fatal(err)
	}
	if ran != 0 || !result.Success || !result.Pinned {
		t.Fatalf("ran = %d result = %+v, want the pin served and marked", ran, result)
	}
	answer, _ := result.ExecutionOutput["json"].(map[string]interface{})
	if answer["aluno"] != "Ana" || answer["token"] == "PINNED" {
		t.Fatalf("output = %v, want the pinned answer with the token still masked", result.ExecutionOutput)
	}
}
