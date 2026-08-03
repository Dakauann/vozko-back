package workflow_usecase

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

func createTestWorkflow(id, workspaceID string, nodes []workflow.Node, edges []workflow.Edge) *workflow.Workflow {
	return &workflow.Workflow{
		ID:          id,
		WorkspaceID: workspaceID,
		Name:        "Test Workflow",
		TriggerType: workflow.TriggerManual,
		Graph: workflow.Graph{
			Nodes: nodes,
			Edges: edges,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

func createTestRegistry() *NodeExecutorRegistry {
	registry := NewNodeExecutorRegistry()

	registry.Register(workflow.NodeTypeActionSetVariable, &mockSetVariableExecutor{})
	registry.Register(workflow.NodeTypeActionHTTPRequest, &mockHTTPRequestExecutor{})
	return registry
}

func TestBuildTestRun_UsesUUIDIdentifiers(t *testing.T) {
	wf := createTestWorkflow("wf1", "ws1", []workflow.Node{{ID: "trigger1", Type: workflow.NodeTypeTriggerManual}}, nil)
	run := buildTestRun(wf, "ws1")

	if _, err := uuid.Parse(run.ID); err != nil {
		t.Fatalf("expected run ID to be a UUID, got %q: %v", run.ID, err)
	}
	if _, err := uuid.Parse(run.EntryID); err != nil {
		t.Fatalf("expected entry ID to be a UUID, got %q: %v", run.EntryID, err)
	}
}

func TestPrepareExecution_UsesSimulationRegistryWhenExecutorDepsAreAvailable(t *testing.T) {
	registry := createTestRegistry()
	wf := createTestWorkflow("wf1", "ws1", []workflow.Node{{ID: "trigger1", Type: workflow.NodeTypeTriggerManual}}, nil)
	uc := &testNodeUseCase{deps: TestNodeDeps{
		Registry: registry,
		ExecutorDeps: ExecutorDeps{
			MessageRepo: &simMessageRepo{},
		},
	}}

	prepared, err := uc.prepareExecution(wf, "ws1", map[string]interface{}{"message": "oi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prepared.registry == registry {
		t.Fatal("expected prepareExecution to use a dedicated simulation registry")
	}
	if prepared.run.EntryType != string(shared.EntryTypeWhatsApp) {
		t.Fatalf("expected simulated entry type %q, got %q", shared.EntryTypeWhatsApp, prepared.run.EntryType)
	}
	if _, err := uuid.Parse(prepared.run.EntryID); err != nil {
		t.Fatalf("expected simulated entry ID to be a UUID, got %q: %v", prepared.run.EntryID, err)
	}
	if got := prepared.run.State.GetString("message"); got != "oi" {
		t.Fatalf("expected trigger variable to be seeded into state, got %q", got)
	}
}

type mockSetVariableExecutor struct{}

func (e *mockSetVariableExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:  workflow.NodeTypeActionSetVariable,
		Label: "Definir Variável",
	}
}

func (e *mockSetVariableExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {

	key, _ := ctx.Node.Config["key"].(string)
	value := ctx.Node.Config["value"]
	if key != "" && ctx.State != nil {
		ctx.State.Set(key, value)
	}
	return &workflow.NodeResult{
		Output: map[string]interface{}{
			"key":   key,
			"value": value,
		},
	}, nil
}

type mockHTTPRequestExecutor struct{}

func (e *mockHTTPRequestExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:  workflow.NodeTypeActionHTTPRequest,
		Label: "Requisição HTTP",
	}
}

func (e *mockHTTPRequestExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {

	url, _ := ctx.Node.Config["url"].(string)
	return &workflow.NodeResult{
		Output: map[string]interface{}{
			"url":         url,
			"status_code": 200,
			"body":        `{"success": true}`,
		},
	}, nil
}

type countingAIExecutor struct {
	executed *int
}

func (e *countingAIExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:  workflow.NodeTypeActionAIAgent,
		Label: "AI Agent",
	}
}

func (e *countingAIExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	if e.executed != nil {
		*e.executed++
	}
	if ctx != nil && ctx.State != nil {
		ctx.State.Set("_ai_response", "live reply")
	}
	return &workflow.NodeResult{
		Output: map[string]interface{}{
			"response": "live reply",
		},
	}, nil
}

// captureHTTPExecutor mimics the real HTTP node: it writes its capture_variable
// into shared state (here, a JSON-array token payload) so downstream nodes can
// index into it.
type captureHTTPExecutor struct{ ran *int }

func (e *captureHTTPExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{Type: workflow.NodeTypeActionHTTPRequest, Label: "Requisição HTTP"}
}

func (e *captureHTTPExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	if e.ran != nil {
		*e.ran++
	}
	if cv, _ := ctx.Node.Config["capture_variable"].(string); cv != "" && ctx.State != nil {
		ctx.State.Set(cv, []interface{}{map[string]interface{}{"token": "REALTOK"}})
	}
	return &workflow.NodeResult{Output: map[string]interface{}{"status_code": 200}}, nil
}

// Reproduces the production node-test gap: testing s2_2 (which needs BOTH a
// mocked AI value AND a non-AI HTTP token from s2_1) must execute s2_1 to
// populate the token, without re-running the mocked AI agent, so the auth
// token interpolates instead of staying an empty/literal Bearer (the 401 cause).
func TestExecute_RunsUpstreamHTTPProducerForCaptureDep(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := NewNodeExecutorRegistry()
	httpRan := 0
	aiRan := 0
	registry.Register(workflow.NodeTypeActionHTTPRequest, &captureHTTPExecutor{ran: &httpRan})
	registry.Register(workflow.NodeTypeActionAIAgent, &countingAIExecutor{executed: &aiRan})

	nodes := []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
		{ID: "ana", Type: workflow.NodeTypeActionAIAgent, Config: map[string]interface{}{"response_variable": "ana_response"}},
		{ID: "s2_1", Type: workflow.NodeTypeActionHTTPRequest, Config: map[string]interface{}{
			"url": "https://tokens.example.com", "method": "GET", "capture_variable": "token_consulta_cadastro",
		}},
		{ID: "s2_2", Type: workflow.NodeTypeActionHTTPRequest, Config: map[string]interface{}{
			"url":        "https://api.example.com/dados",
			"auth_type":  "bearer",
			"auth_token": "{{token_consulta_cadastro[0].token}}",
			"query_params": map[string]interface{}{
				"cpf": "{{ana_response.tool_args.cpf}}",
			},
			"capture_variable": "dados_beneficiario",
		}},
	}
	edges := []workflow.Edge{
		{Source: "trigger1", Target: "ana"},
		{Source: "ana", Target: "s2_1", Label: "consultar_cadastro"},
		{Source: "s2_1", Target: "s2_2", Label: "sucesso"},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, edges)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Registry: registry})

	result, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "s2_2",
		MockedState: map[string]interface{}{"ana_response.tool_args.cpf": "70658062433"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	// The producer (s2_1) must have run so the token resolves; the AI must NOT.
	if aiRan != 0 {
		t.Fatalf("expected mocked AI agent to be skipped, ran %d time(s)", aiRan)
	}
	if got := result.InterpolatedConfig["auth_token"]; got != "REALTOK" {
		t.Fatalf("auth_token did not resolve from upstream producer: got %v, want REALTOK", got)
	}
	if qp, _ := result.InterpolatedConfig["query_params"].(map[string]interface{}); qp["cpf"] != "70658062433" {
		t.Fatalf("cpf mock did not interpolate: got %v", result.InterpolatedConfig["query_params"])
	}
}

func TestAnalyze_NodeWithNoDependencies(t *testing.T) {

	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "greeting",
				"value": "Hello World",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Analyze(context.Background(), AnalyzeNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "setvar1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NodeID != "setvar1" {
		t.Errorf("expected nodeID 'setvar1', got %q", result.NodeID)
	}
	if result.TestMode != TestModeDirect {
		t.Errorf("expected test mode 'direct', got %q", result.TestMode)
	}
	if !result.CanRunDirect {
		t.Error("expected CanRunDirect to be true")
	}
	if len(result.Dependencies) != 0 {
		t.Errorf("expected no dependencies, got %d", len(result.Dependencies))
	}
	if len(result.RequiredMocks) != 0 {
		t.Errorf("expected no required mocks, got %d", len(result.RequiredMocks))
	}
}

func TestAnalyze_NodeWithLastDependency(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "http1",
			Type: workflow.NodeTypeActionHTTPRequest,
			Config: map[string]interface{}{
				"url":    "https://api.example.com",
				"method": "GET",
			},
		},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "result",
				"value": "{{last.body}}",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Analyze(context.Background(), AnalyzeNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "setvar1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TestMode != TestModeExecuteUntil {
		t.Errorf("expected test mode 'execute_until', got %q", result.TestMode)
	}
	if result.CanRunDirect {
		t.Error("expected CanRunDirect to be false")
	}
	if len(result.Dependencies) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(result.Dependencies))
	}
	if result.Dependencies[0].Scope != "last" {
		t.Errorf("expected scope 'last', got %q", result.Dependencies[0].Scope)
	}

	if len(result.RequiredMocks) != 0 {
		t.Errorf("expected 0 required mocks for deterministic dep, got %d", len(result.RequiredMocks))
	}
}

func TestAnalyze_NodeWithAIDependency(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "cep",
				"value": "{{ai.tool_args.cep}}",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Analyze(context.Background(), AnalyzeNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "setvar1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TestMode != TestModeExecuteUntil {
		t.Errorf("expected test mode 'execute_until', got %q", result.TestMode)
	}
	if !result.HasAIDeps {
		t.Error("expected HasAIDeps to be true")
	}
}

func TestAnalyze_NodeWithCustomCaptureVariable(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "bairro",
				"value": "{{respostacep.bairro}}",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Analyze(context.Background(), AnalyzeNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "setvar1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TestMode != TestModeExecuteUntil {
		t.Errorf("expected test mode 'execute_until', got %q", result.TestMode)
	}
	if result.HasAIDeps {
		t.Error("expected HasAIDeps to be false for custom capture variable from non-AI node")
	}
	if len(result.RequiredMocks) != 0 {
		t.Errorf("expected 0 required mocks for deterministic custom capture, got %d", len(result.RequiredMocks))
	}
}

func TestAnalyze_NodeWithAIResponseVariableDependency(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
		{
			ID:   "ai1",
			Type: workflow.NodeTypeActionAIAgent,
			Config: map[string]interface{}{
				"response_variable": "ai_response",
			},
		},
		{
			ID:   "http1",
			Type: workflow.NodeTypeActionHTTPRequest,
			Config: map[string]interface{}{
				"url":  "https://api.example.com/plans",
				"body": "{{ai_response.response_text}}",
			},
		},
	}
	edges := []workflow.Edge{
		{Source: "trigger1", Target: "ai1"},
		{Source: "ai1", Target: "http1"},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, edges)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Registry: registry})

	result, err := uc.Analyze(context.Background(), AnalyzeNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "http1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TestMode != TestModeExecuteUntil {
		t.Fatalf("expected test mode 'execute_until', got %q", result.TestMode)
	}
	if !result.HasAIDeps {
		t.Fatal("expected HasAIDeps to be true for ai response_variable dependency")
	}
	if len(result.RequiredMocks) != 1 {
		t.Fatalf("expected 1 required mock, got %d", len(result.RequiredMocks))
	}
	if result.RequiredMocks[0].DisplayName != "ai_response.response_text" {
		t.Fatalf("expected ai_response.response_text mock, got %q", result.RequiredMocks[0].DisplayName)
	}
}

func TestAnalyze_NodeWithBareAIResponseVariableDependency(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
		{
			ID:   "ai1",
			Type: workflow.NodeTypeActionAIAgent,
			Config: map[string]interface{}{
				"response_variable": "ai_response",
			},
		},
		{
			ID:   "send1",
			Type: workflow.NodeTypeActionSendText,
			Config: map[string]interface{}{
				"text": "Deu um erro ao gerar a resposta...{{ai_response}}",
			},
		},
	}
	edges := []workflow.Edge{
		{Source: "trigger1", Target: "ai1"},
		{Source: "ai1", Target: "send1"},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, edges)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Registry: registry})

	result, err := uc.Analyze(context.Background(), AnalyzeNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "send1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TestMode != TestModeExecuteUntil {
		t.Fatalf("expected test mode 'execute_until', got %q", result.TestMode)
	}
	if result.CanRunDirect {
		t.Fatal("expected bare ai response dependency to require upstream execution or mocks")
	}
	if !result.HasAIDeps {
		t.Fatal("expected HasAIDeps to be true for bare ai response_variable dependency")
	}
	if len(result.RequiredMocks) != 1 {
		t.Fatalf("expected 1 required mock, got %d", len(result.RequiredMocks))
	}
	if result.RequiredMocks[0].DisplayName != "ai_response" {
		t.Fatalf("expected ai_response mock, got %q", result.RequiredMocks[0].DisplayName)
	}
}

func TestAnalyze_NodeWithNodeScopeDependency(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "http1",
			Type: workflow.NodeTypeActionHTTPRequest,
			Config: map[string]interface{}{
				"url": "https://api.example.com",
			},
		},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "prev_result",
				"value": "{{node.http1.body}}",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Analyze(context.Background(), AnalyzeNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "setvar1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TestMode != TestModeExecuteUntil {
		t.Errorf("expected test mode 'execute_until', got %q", result.TestMode)
	}

	if len(result.RequiredMocks) != 0 {
		t.Errorf("expected 0 required mocks for deterministic dep, got %d", len(result.RequiredMocks))
	}
}

func TestAnalyze_NodeWithVarScopeDependency(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "greeting",
				"value": "Hello {{var.name}} and {{message}}",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Analyze(context.Background(), AnalyzeNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "setvar1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TestMode != TestModeDirect {
		t.Errorf("expected test mode 'direct', got %q", result.TestMode)
	}
	if !result.CanRunDirect {
		t.Error("expected CanRunDirect to be true for var scope")
	}
}

func TestAnalyze_WorkflowNotFound(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	_, err := uc.Analyze(context.Background(), AnalyzeNodeInput{
		WorkflowID:  "nonexistent",
		WorkspaceID: "ws1",
		NodeID:      "node1",
	})

	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestAnalyze_WorkspaceMismatch(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
	}
	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	_, err := uc.Analyze(context.Background(), AnalyzeNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws2",
		NodeID:      "trigger1",
	})

	if err == nil {
		t.Fatal("expected error for workspace mismatch")
	}
}

func TestAnalyze_NodeNotFound(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
	}
	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	_, err := uc.Analyze(context.Background(), AnalyzeNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "nonexistent",
	})

	if err == nil {
		t.Fatal("expected error for nonexistent node")
	}
}

func TestExecute_SimpleNode(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "greeting",
				"value": "Hello World",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "setvar1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.InterpolatedConfig["key"] != "greeting" {
		t.Errorf("expected key 'greeting', got %v", result.InterpolatedConfig["key"])
	}
	if result.InterpolatedConfig["value"] != "Hello World" {
		t.Errorf("expected value 'Hello World', got %v", result.InterpolatedConfig["value"])
	}
}

func TestExecute_WithInterpolation(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "greeting",
				"value": "Hello {{name}}!",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "setvar1",
		TriggerVars: map[string]interface{}{
			"name": "Alice",
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.InterpolatedConfig["value"] != "Hello Alice!" {
		t.Errorf("expected value 'Hello Alice!', got %v", result.InterpolatedConfig["value"])
	}
}

func TestExecute_WithMockedState(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "result",
				"value": "Response was: {{last.body}}",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "setvar1",
		MockedState: map[string]interface{}{
			"_last_body": `{"success": true}`,
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.InterpolatedConfig["value"] != `Response was: {"success": true}` {
		t.Errorf("expected interpolated value, got %v", result.InterpolatedConfig["value"])
	}
}

func TestExecute_WithMissingAIMockedStateReturnsValidationError(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "result",
				"value": "AI said: {{ai.response}}",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Registry: registry})

	result, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "setvar1",
		MockedState: map[string]interface{}{"_ai_response": ""},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatalf("expected validation failure for missing AI mocks")
	}
	if !strings.Contains(result.Error, "Forneca valores simulados") {
		t.Fatalf("expected missing mocks validation error, got %q", result.Error)
	}
	if !strings.Contains(result.Error, "ai.response") {
		t.Fatalf("expected AI mock name in error, got %q", result.Error)
	}
}

func TestExecute_ExecuteUntilUsesUpstreamState(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "ai_seed",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "_ai_response",
				"value": "captured reply",
			},
		},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "result",
				"value": "AI: {{ai.response}}",
			},
		},
	}
	edges := []workflow.Edge{
		{Source: "trigger1", Target: "ai_seed"},
		{Source: "ai_seed", Target: "setvar1"},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, edges)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "setvar1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if got := result.InterpolatedConfig["value"]; got != "AI: captured reply" {
		t.Fatalf("expected execute_until interpolation to use upstream state, got %v", got)
	}
	if got := result.StateAfter["_ai_response"]; got != "captured reply" {
		t.Fatalf("expected upstream AI state to be preserved, got %v", got)
	}
}

func TestExecute_ExecuteUntilWithIncompleteMockInputDoesNotRunUpstreamExecution(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()
	executed := 0
	registry.Register(workflow.NodeTypeActionAIAgent, &countingAIExecutor{executed: &executed})

	nodes := []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
		{ID: "ai1", Type: workflow.NodeTypeActionAIAgent, Config: map[string]interface{}{"prompt": "hi"}},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "result",
				"value": "AI: {{ai.response}}",
			},
		},
	}
	edges := []workflow.Edge{
		{Source: "trigger1", Target: "ai1"},
		{Source: "ai1", Target: "setvar1"},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, edges)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "setvar1",
		MockedState: map[string]interface{}{
			"_ai_response": "",
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatalf("expected validation failure for incomplete AI mock input")
	}
	if executed != 0 {
		t.Fatalf("expected upstream AI executor to be skipped, ran %d time(s)", executed)
	}
	if !strings.Contains(result.Error, "Forneca valores simulados") {
		t.Fatalf("expected missing mocks validation error, got %q", result.Error)
	}
	if !strings.Contains(result.Error, "ai.response") {
		t.Fatalf("expected AI mock name in error, got %q", result.Error)
	}
}

func TestExecute_WithMissingAIResponseVariableMockReturnsValidationError(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()
	executed := 0
	registry.Register(workflow.NodeTypeActionAIAgent, &countingAIExecutor{executed: &executed})

	nodes := []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
		{
			ID:   "ai1",
			Type: workflow.NodeTypeActionAIAgent,
			Config: map[string]interface{}{
				"response_variable": "ai_response",
			},
		},
		{
			ID:   "http1",
			Type: workflow.NodeTypeActionHTTPRequest,
			Config: map[string]interface{}{
				"url":  "https://api.example.com/plans",
				"body": "{{ai_response.response_text}}",
			},
		},
	}
	edges := []workflow.Edge{
		{Source: "trigger1", Target: "ai1"},
		{Source: "ai1", Target: "http1"},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, edges)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Registry: registry})

	result, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "http1",
		MockedState: map[string]interface{}{
			"ai_response.response_text": "",
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected validation failure for missing AI response_variable mock")
	}
	if executed != 0 {
		t.Fatalf("expected upstream AI executor to be skipped, ran %d time(s)", executed)
	}
	if !strings.Contains(result.Error, "Forneca valores simulados") {
		t.Fatalf("expected missing mocks validation error, got %q", result.Error)
	}
	if !strings.Contains(result.Error, "ai_response.response_text") {
		t.Fatalf("expected custom AI mock name in error, got %q", result.Error)
	}
}

func TestExecute_WithMissingBareAIResponseVariableMockReturnsValidationError(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()
	executed := 0
	registry.Register(workflow.NodeTypeActionAIAgent, &countingAIExecutor{executed: &executed})

	nodes := []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
		{
			ID:   "ai1",
			Type: workflow.NodeTypeActionAIAgent,
			Config: map[string]interface{}{
				"response_variable": "ai_response",
			},
		},
		{
			ID:   "send1",
			Type: workflow.NodeTypeActionSendText,
			Config: map[string]interface{}{
				"text": "Deu um erro ao gerar a resposta...{{ai_response}}",
			},
		},
	}
	edges := []workflow.Edge{
		{Source: "trigger1", Target: "ai1"},
		{Source: "ai1", Target: "send1"},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, edges)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Registry: registry})

	result, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "send1",
		MockedState: map[string]interface{}{
			"ai_response": "",
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected validation failure for missing bare AI response_variable mock")
	}
	if executed != 0 {
		t.Fatalf("expected upstream AI executor to be skipped, ran %d time(s)", executed)
	}
	if !strings.Contains(result.Error, "Forneca valores simulados") {
		t.Fatalf("expected missing mocks validation error, got %q", result.Error)
	}
	if !strings.Contains(result.Error, "ai_response") {
		t.Fatalf("expected bare AI mock name in error, got %q", result.Error)
	}
	if strings.Contains(result.Error, "ai_response.response_text") {
		t.Fatalf("expected bare AI mock name only, got %q", result.Error)
	}
}

func TestExecute_WithNestedAIMockedState(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "cep",
				"value": "{{ai.tool_args.cep}}",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Registry: registry})

	result, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "setvar1",
		MockedState: map[string]interface{}{
			"_ai_tool_args.cep": "01001-000",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if got := result.InterpolatedConfig["value"]; got != "01001-000" {
		t.Fatalf("expected nested AI mock interpolation, got %v", got)
	}
}

func TestExecute_ExecuteUntilUsesProvidedCustomMocksWithoutReachingTarget(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
		{
			ID:   "calendar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "date",
				"value": "{{ai_response.tool_args.dia_e_hora_para_checar}}",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Registry: registry})

	result, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "calendar1",
		MockedState: map[string]interface{}{
			"ai_response.tool_args.dia_e_hora_para_checar": "2026-04-14T10:00:00-03:00",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if got := result.InterpolatedConfig["value"]; got != "2026-04-14T10:00:00-03:00" {
		t.Fatalf("expected custom capture mock interpolation, got %v", got)
	}
}

func TestExecute_SkipExecutionFlag(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "test",
				"value": "Hello {{name}}",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:    "wf1",
		WorkspaceID:   "ws1",
		NodeID:        "setvar1",
		TriggerVars:   map[string]interface{}{"name": "Bob"},
		SkipExecution: true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}

	if result.InterpolatedConfig["value"] != "Hello Bob" {
		t.Errorf("expected interpolated value, got %v", result.InterpolatedConfig["value"])
	}

	if result.ExecutionOutput != nil {
		t.Errorf("expected nil execution output when skipped, got %v", result.ExecutionOutput)
	}
}

func TestExecute_MergesOutputsIntoStateAfter(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{
				"key":   "greeting",
				"value": "Hello World",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "setvar1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if got := result.StateAfter["greeting"]; got != "Hello World" {
		t.Fatalf("expected executor state mutation to persist, got %v", got)
	}
	if got := result.StateAfter["_last_key"]; got != "greeting" {
		t.Fatalf("expected _last_key to be merged, got %v", got)
	}
	if got := result.StateAfter["_last_value"]; got != "Hello World" {
		t.Fatalf("expected _last_value to be merged, got %v", got)
	}
	if got := result.StateAfter["_node_setvar1_value"]; got != "Hello World" {
		t.Fatalf("expected node-scoped output to be merged, got %v", got)
	}
}

func TestExecute_HTTPRequest(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "http1",
			Type: workflow.NodeTypeActionHTTPRequest,
			Config: map[string]interface{}{
				"url":    "https://api.example.com/users/{{user_id}}",
				"method": "GET",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "http1",
		TriggerVars: map[string]interface{}{
			"user_id": "123",
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.InterpolatedConfig["url"] != "https://api.example.com/users/123" {
		t.Errorf("expected interpolated URL, got %v", result.InterpolatedConfig["url"])
	}
}

func TestExecute_NoExecutor(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "unknown1",
			Type: "unknown_node_type",
			Config: map[string]interface{}{
				"key": "value",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "unknown1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure when no executor exists")
	}
	if result.Error == "" {
		t.Error("expected error message")
	}
}

func TestExecute_WorkflowNotFound(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	_, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "nonexistent",
		WorkspaceID: "ws1",
		NodeID:      "node1",
	})

	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestExecute_WorkspaceMismatch(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
	}
	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	_, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws2",
		NodeID:      "trigger1",
	})

	if err == nil {
		t.Fatal("expected error for workspace mismatch")
	}
}

func TestExecute_NodeNotFound(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
	}
	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	_, err := uc.Execute(context.Background(), TestNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "nonexistent",
	})

	if err == nil {
		t.Fatal("expected error for nonexistent node")
	}
}

func TestToUIAnalysis_DirectMode(t *testing.T) {
	analysis := &AnalyzeNodeOutput{
		NodeID:        "node1",
		NodeType:      workflow.NodeTypeActionSetVariable,
		NodeLabel:     "Definir Variável",
		TestMode:      TestModeDirect,
		CanRunDirect:  true,
		HasAIDeps:     false,
		Dependencies:  nil,
		RequiredMocks: nil,
	}

	ui := analysis.ToUIAnalysis()

	if ui.TestMode != TestModeDirect {
		t.Errorf("expected test mode 'direct', got %q", ui.TestMode)
	}
	if !ui.CanRunDirect {
		t.Error("expected CanRunDirect to be true")
	}
	if len(ui.MockFields) != 0 {
		t.Errorf("expected no mock fields, got %d", len(ui.MockFields))
	}
	if ui.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestToUIAnalysis_ExecuteUntilDeterministic(t *testing.T) {
	analysis := &AnalyzeNodeOutput{
		NodeID:        "node1",
		NodeType:      workflow.NodeTypeActionSetVariable,
		NodeLabel:     "Definir Variável",
		TestMode:      TestModeExecuteUntil,
		CanRunDirect:  false,
		HasAIDeps:     false,
		RequiredMocks: nil,
	}

	ui := analysis.ToUIAnalysis()

	if ui.TestMode != TestModeExecuteUntil {
		t.Errorf("expected test mode 'execute_until', got %q", ui.TestMode)
	}
	if len(ui.MockFields) != 0 {
		t.Errorf("expected 0 mock fields for deterministic deps, got %d", len(ui.MockFields))
	}
	if !strings.Contains(ui.Message, "automaticamente") {
		t.Errorf("expected auto-execute message, got %q", ui.Message)
	}
}

func TestToUIAnalysis_ExecuteUntilMode(t *testing.T) {
	analysis := &AnalyzeNodeOutput{
		NodeID:       "node1",
		NodeType:     workflow.NodeTypeActionSetVariable,
		NodeLabel:    "Definir Variável",
		TestMode:     TestModeExecuteUntil,
		CanRunDirect: false,
		HasAIDeps:    true,
		RequiredMocks: []workflow.RequiredMock{
			{
				StateKey:    "_ai_response",
				DisplayName: "ai.response",
				Source:      workflow.DependencySourceAI,
			},
		},
	}

	ui := analysis.ToUIAnalysis()

	if ui.TestMode != TestModeExecuteUntil {
		t.Errorf("expected test mode 'execute_until', got %q", ui.TestMode)
	}
	if !ui.HasAIDeps {
		t.Error("expected HasAIDeps to be true")
	}
}

func TestAnalyze_MixedDependencies(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "setvar1",
			Type: workflow.NodeTypeActionSetVariable,
			Config: map[string]interface{}{

				"key":   "complex",
				"value": "User {{name}}, HTTP: {{last.body}}, AI: {{ai.response}}",
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Analyze(context.Background(), AnalyzeNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "setvar1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TestMode != TestModeExecuteUntil {
		t.Errorf("expected test mode 'execute_until' for mixed deps with AI, got %q", result.TestMode)
	}
	if !result.HasAIDeps {
		t.Error("expected HasAIDeps to be true")
	}

	if len(result.Dependencies) != 3 {
		t.Errorf("expected 3 dependencies, got %d", len(result.Dependencies))
	}
}

func TestAnalyze_NestedConfig(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := createTestRegistry()

	nodes := []workflow.Node{
		{
			ID:   "trigger1",
			Type: workflow.NodeTypeTriggerManual,
		},
		{
			ID:   "http1",
			Type: workflow.NodeTypeActionHTTPRequest,
			Config: map[string]interface{}{
				"url":    "https://api.example.com",
				"method": "POST",
				"headers": map[string]interface{}{
					"Authorization": "Bearer {{var.token}}",
					"X-Request-ID":  "{{last.request_id}}",
				},
				"body": map[string]interface{}{
					"user_id": "{{node.prev.user_id}}",
					"data":    "static value",
				},
			},
		},
	}

	wf := createTestWorkflow("wf1", "ws1", nodes, nil)
	repo.Create(wf)

	uc := NewTestNodeUseCase(TestNodeDeps{
		WorkflowRepo: repo,
		Registry:     registry,
	})

	result, err := uc.Analyze(context.Background(), AnalyzeNodeInput{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		NodeID:      "http1",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Dependencies) != 3 {
		t.Errorf("expected 3 dependencies from nested config, got %d: %+v", len(result.Dependencies), result.Dependencies)
	}

	if result.TestMode != TestModeExecuteUntil {
		t.Errorf("expected test mode 'execute_until', got %q", result.TestMode)
	}
}
