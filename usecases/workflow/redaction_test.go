package workflow_usecase

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/redact"
	"vozko/domain/workflow"
)

type credentialEchoExecutor struct{}

func (credentialEchoExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{Type: workflow.NodeTypeActionHTTPRequest}
}

func (credentialEchoExecutor) Execute(*workflow.NodeContext) (*workflow.NodeResult, error) {
	return &workflow.NodeResult{Output: map[string]interface{}{
		"accessToken": "tok-123",
		"cpf":         "529.982.247-25",
		"nome":        "Ana",
		"body":        `{"refresh_token":"r-9"}`,
	}}, nil
}

type failingCredentialExecutor struct{}

func (failingCredentialExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{Type: workflow.NodeTypeActionCode}
}

func (failingCredentialExecutor) Execute(*workflow.NodeContext) (*workflow.NodeResult, error) {
	return &workflow.NodeResult{Error: `login refused: {"password":"p-1"}`}, nil
}

func echoThenFailWorkflow() *workflow.Workflow {
	return &workflow.Workflow{
		ID: "wf-1", WorkspaceID: "ws-1", Type: workflow.WorkflowTypeMessages,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "n1", Type: workflow.NodeTypeTriggerWebhook},
				{ID: "n55", Type: workflow.NodeTypeActionHTTPRequest},
				{ID: "n56", Type: workflow.NodeTypeActionCode},
			},
			Edges: []workflow.Edge{{Source: "n1", Target: "n55"}, {Source: "n55", Target: "n56"}},
		},
	}
}

func TestStepLogsAreRedactedWhileTheRunKeepsRealValues(t *testing.T) {
	registry := NewNodeExecutorRegistry()
	registry.Register(workflow.NodeTypeActionHTTPRequest, credentialEchoExecutor{})
	registry.Register(workflow.NodeTypeActionCode, failingCredentialExecutor{})
	w := echoThenFailWorkflow()
	run := &workflow.WorkflowRun{ID: "run-1", WorkflowID: w.ID, WorkspaceID: w.WorkspaceID, Status: workflow.RunStatusRunning, CurrentNodeID: "n1", State: workflow.NewRunState()}
	runs := NewMockWorkflowRunRepository()
	_ = runs.Create(run)
	logs := NewMockWorkflowRunLogRepository()

	if err := NewRunEngine(runs, logs, registry).Execute(run, w); err != nil {
		t.Fatal(err)
	}
	if got, _ := run.State.Get("_node_n55_accessToken"); got != "tok-123" {
		t.Fatalf("the run needs the real token in its state, got %v", got)
	}

	entries, _ := logs.FindByRunID("run-1")
	var sawOutput, sawError bool
	for _, e := range entries {
		if e.NodeID == "n55" {
			sawOutput = true
			if e.Output["accessToken"] != redact.Mask || e.Output["nome"] != "Ana" {
				t.Fatalf("step output not redacted by key: %v", e.Output)
			}
			if e.Output["cpf"] != "•••.•••.•••-25" {
				t.Fatalf("cpf must keep only the last two digits: %v", e.Output["cpf"])
			}
			if strings.Contains(e.Output["body"].(string), "r-9") {
				t.Fatalf("credential inside a body string leaked: %v", e.Output["body"])
			}
		}
		if e.NodeID == "n56" {
			sawError = true
			if strings.Contains(e.Error, "p-1") {
				t.Fatalf("step error leaked a password: %q", e.Error)
			}
		}
	}
	if !sawOutput || !sawError {
		t.Fatalf("expected both step rows, got %+v", entries)
	}
}

func runWithCredentialsInState() (*MockWorkflowRunRepository, *MockWorkflowRunLogRepository) {
	runs, run := runOwnedBy("ws-owner")
	run.State.Set("unifecaf_secret", "s-1")
	run.State.Set("unifecaf_access_token", "t-1")
	run.State.Set("_node_n55_body", `{"accessToken":"t-1"}`)
	run.State.Set("cpf", "52998224725")
	run.State.Set("curso", "ADS")
	_ = runs.Update(run)
	logs := NewMockWorkflowRunLogRepository()
	_ = logs.Create(&workflow.WorkflowRunLog{ID: "l1", RunID: "run-1", NodeID: "n55", Output: map[string]interface{}{"accessToken": "t-1"}, Error: "Bearer t-1 refused"})
	return runs, logs
}

func TestGetRunReturnsARedactedCopyAndLeavesTheStoredRunAlone(t *testing.T) {
	runs, logs := runWithCredentialsInState()
	detail, err := NewGetRunUseCase(runs, logs, seesEverything{}).Execute(viewer, "ws-owner", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	vars := detail.Run.State.Vars
	if vars["unifecaf_secret"] != redact.Mask || vars["unifecaf_access_token"] != redact.Mask || vars["curso"] != "ADS" {
		t.Fatalf("run state not redacted: %v", vars)
	}
	if strings.Contains(vars["_node_n55_body"].(string), "t-1") || vars["cpf"] != "•••••••••25" {
		t.Fatalf("embedded token or cpf leaked: %v", vars)
	}
	if detail.Logs[0].Output["accessToken"] != redact.Mask || strings.Contains(detail.Logs[0].Error, "t-1") {
		t.Fatalf("logs not redacted: %+v", detail.Logs[0])
	}
	stored, _ := runs.FindByID("run-1")
	if stored.State.Vars["unifecaf_access_token"] != "t-1" {
		t.Fatal("redaction must never touch the stored run")
	}
}

func TestListRunsRedactsEachRunState(t *testing.T) {
	runs, _ := runWithCredentialsInState()
	result, err := NewListRunsUseCase(runs, seesEverything{}).Execute(viewer, workflow.ListRunsInput{WorkspaceID: "ws-owner", WorkflowID: "wf-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].State.Vars["unifecaf_access_token"] != redact.Mask {
		t.Fatalf("listed run state not redacted: %+v", result.Items)
	}
	stored, _ := runs.FindByID("run-1")
	if stored.State.Vars["unifecaf_access_token"] != "t-1" {
		t.Fatal("redaction must never touch the stored run")
	}
}

func TestNodeTestResponseIsRedacted(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := NewNodeExecutorRegistry()
	registry.Register(workflow.NodeTypeActionHTTPRequest, credentialEchoExecutor{})
	wf := createTestWorkflow("wf1", "ws1", []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerMessageReceived},
		{ID: "h1", Type: workflow.NodeTypeActionHTTPRequest, Config: map[string]interface{}{"url": "https://api.test/alunos/{{cpf}}", "auth_token": "{{senha_informada}}"}},
	}, []workflow.Edge{{Source: "trigger1", Target: "h1"}})
	_ = repo.Create(wf)

	result, err := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Steps: registry, Executors: fixedExecutors(registry)}).Execute(context.Background(), TestNodeInput{
		WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "h1", ConfirmRealCalls: true,
		TriggerVars: map[string]interface{}{"senha_informada": "p4ss", "cpf": "529.982.247-25"},
	})
	if err != nil || !result.Success {
		t.Fatalf("unexpected failure: %v %+v", err, result)
	}
	if result.InterpolatedConfig["url"] != "https://api.test/alunos/•••.•••.•••-25" || result.InterpolatedConfig["auth_token"] != redact.Mask {
		t.Fatalf("interpolated config not redacted: %v", result.InterpolatedConfig)
	}
	if result.ExecutionOutput["accessToken"] != redact.Mask || result.ExecutionOutput["nome"] != "Ana" {
		t.Fatalf("execution output not redacted: %v", result.ExecutionOutput)
	}
	if result.StateAfter["_node_h1_accessToken"] != redact.Mask || result.StateAfter["senha_informada"] != redact.Mask {
		t.Fatalf("state_after not redacted: %v", result.StateAfter)
	}
}
