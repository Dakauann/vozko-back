package tools_usecase

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/agent"
	"vozko/domain/tools"
	"vozko/domain/workflow"
	"vozko/usecases/agentctx"
	workflow_usecase "vozko/usecases/workflow"
)

type workflowFinderStub struct {
	workflows map[string]*workflow.Workflow
	published map[string]workflow.Graph
	err       error
}

func (s *workflowFinderStub) FindByID(id string) (*workflow.Workflow, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.workflows[id], nil
}

func (s *workflowFinderStub) FindVersion(id string, version int) (*workflow.WorkflowVersion, error) {
	w, ok := s.workflows[id]
	if !ok {
		return nil, nil
	}
	g := w.Graph.Clone()
	if published, ok := s.published[id]; ok {
		g = published.Clone()
	}
	return &workflow.WorkflowVersion{WorkflowID: id, Version: version, Type: w.Type, Graph: &g}, nil
}

type workflowStarterStub struct {
	starts  []workflow_usecase.ConversationStart
	started workflow_usecase.StartedRun
	err     error
}

func (s *workflowStarterStub) StartForConversation(in workflow_usecase.ConversationStart) (workflow_usecase.StartedRun, error) {
	s.starts = append(s.starts, in)
	if s.err != nil {
		return workflow_usecase.StartedRun{}, s.err
	}
	started := s.started
	if started.RunID == "" {
		started.RunID = "run-" + in.WorkflowID
	}
	return started, nil
}

type stepNamesStub map[workflow.NodeType]string

func (s stepNamesStub) StepName(t workflow.NodeType) string {
	if name, ok := s[t]; ok {
		return name
	}
	return string(t)
}

func callableWorkflow(id, name string, inputs ...interface{}) *workflow.Workflow {
	published := 1
	return &workflow.Workflow{
		ID: id, WorkspaceID: "ws-1", Name: name, Description: "Faz a matrícula do candidato.",
		Status: workflow.WorkflowStatusActive, Type: workflow.WorkflowTypeMessages, PublishedVersion: &published,
		Graph: workflow.Graph{Nodes: []workflow.Node{
			{ID: "t", Type: workflow.NodeTypeTriggerAgentCall, Config: map[string]interface{}{workflow.AgentCallInputsKey: inputs}},
			{ID: "end", Type: workflow.NodeTypeEnd},
		}},
	}
}

func workflowInput(name, typ string, required bool) map[string]interface{} {
	return map[string]interface{}{"name": name, "type": typ, "required": required, "description": "campo " + name}
}

type runWorkflowHarness struct {
	finder  *workflowFinderStub
	starter *workflowStarterStub
	tool    tools.Handler
}

func newRunWorkflowHarness() *runWorkflowHarness {
	h := &runWorkflowHarness{
		finder: &workflowFinderStub{workflows: map[string]*workflow.Workflow{
			"wf-a": callableWorkflow("wf-a", "Matrícula", workflowInput("cpf", "string", true), workflowInput("idade", "number", false)),
			"wf-b": callableWorkflow("wf-b", "Segunda via", workflowInput("boleto", "string", true)),
		}},
		starter: &workflowStarterStub{},
	}
	tool := NewRunWorkflowToolUseCase(h.finder, h.starter, stepNamesStub{workflow.NodeTypeActionHTTPRequest: "Requisição HTTP"})
	tool.(*runWorkflowTool).now = func() time.Time { return time.Date(2026, 10, 19, 20, 20, 0, 0, time.UTC) }
	h.tool = tool
	return h
}

func conversationSeed(workflowID string) map[string]interface{} {
	return map[string]interface{}{
		"workflow_id":    workflowID,
		"__workspace_id": "ws-1",
		"__entry_id":     "e-1",
		"__entry_type":   "instagram",
		"__agent_id":     "agent-1",
	}
}

func agentContext() context.Context {
	return agentctx.WithAgent(context.Background(), &agent.Agent{ID: "agent-1", WorkspaceID: "ws-1", Name: "Bia"})
}

func resultText(t *testing.T, r tools.ExecutionResult) string {
	t.Helper()
	text, ok := r.Result.(string)
	if !ok {
		t.Fatalf("result is not text: %#v", r.Result)
	}
	return text
}

func TestRunWorkflowIsAMessagingToolBoundToOneWorkflowPerBinding(t *testing.T) {
	def := newRunWorkflowHarness().tool.Definition()
	if def.Name != RunWorkflowToolName || def.DisplayName != "Executar fluxo" || def.BindingKey != "workflow_id" {
		t.Fatalf("definition = %+v", def)
	}
	if !def.IsVisibleIn(tools.VisibilityMessaging) || def.IsVisibleIn(tools.VisibilityPostConversation) || def.IsVisibleIn(tools.VisibilityAnalysis) {
		t.Fatalf("visibility = %v, want messaging only", def.Visibility)
	}
	setting := def.ConfigSchema["workflow_id"]
	if setting.OptionsSource != AgentWorkflowsOptionsSource || !setting.Required || !reflect.DeepEqual(def.RequiredConfig, []string{"workflow_id"}) {
		t.Fatalf("workflow_id setting = %+v, required = %v", setting, def.RequiredConfig)
	}
}

func TestTheModelSeesTheWorkflowsNameDescriptionAndInputs(t *testing.T) {
	h := newRunWorkflowHarness()
	def := h.tool.(tools.ContextualHandler).DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws-1", Config: map[string]interface{}{"workflow_id": "wf-a"}})

	for _, fragment := range []string{`"Matrícula"`, "Faz a matrícula do candidato.", "- cpf (string) [REQUIRED]: campo cpf", "- idade (number): campo idade"} {
		if !strings.Contains(def.Description, fragment) {
			t.Fatalf("description misses %q:\n%s", fragment, def.Description)
		}
	}
	if def.Parameters["inputs"].Type != "object" || !strings.Contains(def.Parameters["inputs"].Description, "cpf: string (required)") {
		t.Fatalf("inputs = %+v", def.Parameters["inputs"])
	}
	if !reflect.DeepEqual(def.Required, []string{"inputs"}) {
		t.Fatalf("required = %v", def.Required)
	}
}

func TestAWorkflowOutsideTheWorkspaceIsNotDescribed(t *testing.T) {
	h := newRunWorkflowHarness()
	h.finder.workflows["wf-x"] = callableWorkflow("wf-x", "Alheio", workflowInput("cpf", "string", true))
	h.finder.workflows["wf-x"].WorkspaceID = "ws-other"

	def := h.tool.(tools.ContextualHandler).DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws-1", Config: map[string]interface{}{"workflow_id": "wf-x"}})
	if strings.Contains(def.Description, "Alheio") || def.Parameters["inputs"].Description != "" && strings.Contains(def.Parameters["inputs"].Description, "cpf") {
		t.Fatalf("another workspace's workflow leaked: %+v", def)
	}
	if !strings.Contains(def.Description, "indisponível") {
		t.Fatalf("description = %q", def.Description)
	}
}

func TestRunWorkflowStartsTheBoundWorkflowOnTheCurrentConversation(t *testing.T) {
	h := newRunWorkflowHarness()

	result, err := h.tool.ExecuteWithConfig(agentContext(), conversationSeed("wf-a"), map[string]interface{}{
		"inputs": map[string]interface{}{"cpf": "52998224725", "idade": float64(30), "senha": "x"},
	})
	if err != nil || result.IsError {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if got := resultText(t, result); got != "Fluxo iniciado. Não afirme conclusão; o resultado chega depois." {
		t.Fatalf("text = %q", got)
	}
	if result.RunID != "run-wf-a" {
		t.Fatalf("run id = %q", result.RunID)
	}
	start := h.starter.starts[0]
	if start.WorkspaceID != "ws-1" || start.WorkflowID != "wf-a" || start.EntryID != "e-1" || start.EntryType != "instagram" {
		t.Fatalf("start = %+v", start)
	}
	if start.TriggerType != workflow.TriggerAgentCall || start.StartedBy != "agent:agent-1" {
		t.Fatalf("start = %+v", start)
	}
	wantData := map[string]interface{}{
		"input": map[string]interface{}{"cpf": "52998224725", "idade": float64(30)},
		"agent": map[string]interface{}{"id": "agent-1", "name": "Bia"},
	}
	if !reflect.DeepEqual(start.Data, wantData) {
		t.Fatalf("data = %#v", start.Data)
	}
}

func TestRunWorkflowSaysSinceWhenAndWhereAnAlreadyRunningFlowIs(t *testing.T) {
	h := newRunWorkflowHarness()
	h.starter.started = workflow_usecase.StartedRun{
		RunID: "run-old", AlreadyRunning: true, StartedAt: time.Date(2026, 10, 19, 20, 15, 0, 0, time.UTC),
		CurrentNodeType: workflow.NodeTypeActionHTTPRequest,
	}

	result, _ := h.tool.ExecuteWithConfig(agentContext(), conversationSeed("wf-a"), map[string]interface{}{"inputs": map[string]interface{}{"cpf": "1"}})
	text := resultText(t, result)
	for _, fragment := range []string{"já está em execução", "há 5 min", "Requisição HTTP", "Não inicie de novo"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("text misses %q: %q", fragment, text)
		}
	}
	if result.RunID != "run-old" || result.IsError {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunWorkflowRefusalsAreToldToTheModelInPortuguese(t *testing.T) {
	cases := []struct {
		name       string
		config     map[string]interface{}
		inputs     interface{}
		starterErr error
		want       string
		starts     int
	}{
		{"inactive", conversationSeed("wf-a"), map[string]interface{}{"cpf": "1"}, workflow.ErrWorkflowNotActive, "Fluxo inativo. Não prometa; diga que vai verificar.", 1},
		{"missing fields", conversationSeed("wf-a"), map[string]interface{}{"idade": float64(3)}, nil, "cpf", 0},
		{"wrong type", conversationSeed("wf-a"), map[string]interface{}{"cpf": float64(1)}, nil, "cpf deve ser string", 0},
		{"inputs not an object", conversationSeed("wf-a"), "cpf=1", nil, "objeto", 0},
		{"no conversation", map[string]interface{}{"workflow_id": "wf-a", "__agent_id": "agent-1"}, map[string]interface{}{"cpf": "1"}, nil, "conversa atual", 0},
		{"workflow of another workspace", func() map[string]interface{} { c := conversationSeed("wf-a"); c["__workspace_id"] = "ws-2"; return c }(), map[string]interface{}{"cpf": "1"}, nil, "não encontrado", 0},
		{"unknown workflow", conversationSeed("wf-gone"), map[string]interface{}{"cpf": "1"}, nil, "não encontrado", 0},
		{"workflow without the agent trigger", conversationSeed("wf-a"), map[string]interface{}{"cpf": "1"}, workflow.ErrTriggerNotInWorkflow, "não aceita", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newRunWorkflowHarness()
			h.starter.err = tc.starterErr
			result, err := h.tool.ExecuteWithConfig(agentContext(), tc.config, map[string]interface{}{"inputs": tc.inputs})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError || !strings.Contains(resultText(t, result), tc.want) {
				t.Fatalf("result = %+v, want an error mentioning %q", result, tc.want)
			}
			if len(h.starter.starts) != tc.starts {
				t.Fatalf("starts = %d, want %d", len(h.starter.starts), tc.starts)
			}
		})
	}
}

func TestTwoWorkflowBindingsBecomeTwoToolsAndEachCallStartsItsOwnWorkflow(t *testing.T) {
	h := newRunWorkflowHarness()
	registry := NewService(h.tool)
	bindings := []agent.ToolBinding{
		{Name: RunWorkflowToolName, Config: map[string]interface{}{"workflow_id": "wf-a"}},
		{Name: RunWorkflowToolName, Config: map[string]interface{}{"workflow_id": "wf-b"}},
		{Name: RunWorkflowToolName, Config: map[string]interface{}{"workflow_id": "wf-a"}},
	}

	resolved := ResolveTools(registry, bindings, agent.ToolVisibilityMessaging, ToolResolverOptions{Agent: &agent.Agent{ID: "agent-1", WorkspaceID: "ws-1"}})

	if len(resolved.Definitions) != 2 {
		t.Fatalf("definitions = %+v, want one tool per bound workflow", resolved.Definitions)
	}
	byWorkflow := map[string]tools.Definition{}
	for _, def := range resolved.Definitions {
		if !tools.ValidToolName(def.Name) || def.HandlerName != RunWorkflowToolName {
			t.Fatalf("tool %q is not a valid bound name of run_workflow", def.Name)
		}
		byWorkflow[resolved.Configs[strings.ToLower(def.Name)]["workflow_id"].(string)] = def
	}
	if !strings.Contains(byWorkflow["wf-a"].Description, "Matrícula") || !strings.Contains(byWorkflow["wf-b"].Description, "Segunda via") {
		t.Fatalf("each tool must describe its own workflow: %+v", byWorkflow)
	}

	for workflowID, field := range map[string]string{"wf-a": "cpf", "wf-b": "boleto"} {
		def := byWorkflow[workflowID]
		config := map[string]interface{}{}
		for k, v := range resolved.Configs[strings.ToLower(def.Name)] {
			config[k] = v
		}
		for k, v := range conversationSeed("") {
			if k != "workflow_id" {
				config[k] = v
			}
		}
		result, err := registry.ExecuteWithConfig(agentContext(), def.Name, config, map[string]interface{}{"inputs": map[string]interface{}{field: "1"}})
		if err != nil || result.IsError || result.RunID != "run-"+workflowID {
			t.Fatalf("calling %s: result = %+v, err = %v", def.Name, result, err)
		}
	}
	if len(h.starter.starts) != 2 || h.starter.starts[0].WorkflowID == h.starter.starts[1].WorkflowID {
		t.Fatalf("starts = %+v", h.starter.starts)
	}
	if _, ok := registry.Handler(byWorkflow["wf-b"].Name); !ok {
		t.Fatal("a bound name must route to its handler")
	}
}

func TestNewRunWorkflowToolNeedsItsDependencies(t *testing.T) {
	if NewRunWorkflowToolUseCase(nil, &workflowStarterStub{}, stepNamesStub{}) != nil {
		t.Fatal("expected no tool without the workflow finder")
	}
	if NewRunWorkflowToolUseCase(&workflowFinderStub{}, nil, stepNamesStub{}) != nil {
		t.Fatal("expected no tool without the starter")
	}
	if NewRunWorkflowToolUseCase(&workflowFinderStub{}, &workflowStarterStub{}, nil) != nil {
		t.Fatal("expected no tool without the step names")
	}
}

func TestAnUnreadableWorkflowFailsClosed(t *testing.T) {
	h := newRunWorkflowHarness()
	h.finder.err = errors.New("db down")
	result, _ := h.tool.ExecuteWithConfig(agentContext(), conversationSeed("wf-a"), map[string]interface{}{"inputs": map[string]interface{}{"cpf": "1"}})
	if !result.IsError || len(h.starter.starts) != 0 {
		t.Fatalf("result = %+v, starts = %d", result, len(h.starter.starts))
	}
}

func TestTheModelSeesThePublishedInputsNotTheDraftOnes(t *testing.T) {
	h := newRunWorkflowHarness()
	published := callableWorkflow("wf-a", "Matrícula", workflowInput("rg", "string", true)).Graph
	h.finder.published = map[string]workflow.Graph{"wf-a": published}

	def := h.tool.(tools.ContextualHandler).DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws-1", Config: map[string]interface{}{"workflow_id": "wf-a"}})
	if !strings.Contains(def.Description, "- rg (string) [REQUIRED]") {
		t.Fatalf("description = %q, want the published field rg", def.Description)
	}
	if strings.Contains(def.Description, "cpf") {
		t.Fatal("a field only in the draft must not reach the model, the run would not accept it")
	}

	res, err := h.tool.ExecuteWithConfig(agentContext(), conversationSeed("wf-a"), map[string]interface{}{"inputs": map[string]interface{}{"cpf": "123"}})
	if err != nil {
		t.Fatal(err)
	}
	if text := resultText(t, res); !strings.Contains(text, "rg") || len(h.starter.starts) != 0 {
		t.Fatalf("result = %q starts = %d, want the published required field asked for", text, len(h.starter.starts))
	}
}

func TestANeverPublishedWorkflowIsUnavailableToTheAgent(t *testing.T) {
	h := newRunWorkflowHarness()
	h.finder.workflows["wf-a"].PublishedVersion = nil
	h.finder.workflows["wf-a"].Status = workflow.WorkflowStatusDraft

	def := h.tool.(tools.ContextualHandler).DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws-1", Config: map[string]interface{}{"workflow_id": "wf-a"}})
	if def.Description != runWorkflowUnavailable {
		t.Fatalf("description = %q", def.Description)
	}
}

func TestAPausedWorkflowIsNeitherAdvertisedNorStarted(t *testing.T) {
	h := newRunWorkflowHarness()
	h.finder.workflows["wf-a"].Status = workflow.WorkflowStatusPaused

	def := h.tool.(tools.ContextualHandler).DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws-1", Config: map[string]interface{}{"workflow_id": "wf-a"}})
	if def.Description != runWorkflowUnavailable {
		t.Fatalf("description = %q, a paused workflow must not be advertised as callable", def.Description)
	}
	result, err := h.tool.ExecuteWithConfig(agentContext(), conversationSeed("wf-a"), map[string]interface{}{"inputs": map[string]interface{}{"cpf": "1"}})
	if err != nil || result.ErrorCode != workflow.WorkflowNotActiveCode || len(h.starter.starts) != 0 {
		t.Fatalf("result = %+v err = %v starts = %d", result, err, len(h.starter.starts))
	}
}

func TestOnlyAnInactiveRefusalCarriesTheWorkflowNotActiveCodeForTheRuntimeAlert(t *testing.T) {
	h := newRunWorkflowHarness()
	h.starter.err = workflow.ErrWorkflowNotActive
	inactive, _ := h.tool.ExecuteWithConfig(agentContext(), conversationSeed("wf-a"), map[string]interface{}{"inputs": map[string]interface{}{"cpf": "1"}})
	if inactive.ErrorCode != workflow.WorkflowNotActiveCode {
		t.Fatalf("inactive refusal code = %q", inactive.ErrorCode)
	}

	h = newRunWorkflowHarness()
	missing, _ := h.tool.ExecuteWithConfig(agentContext(), conversationSeed("wf-a"), map[string]interface{}{"inputs": map[string]interface{}{"idade": float64(3)}})
	if !missing.IsError || missing.ErrorCode != "" {
		t.Fatalf("a missing field is the model's mistake, not an inactive workflow: %+v", missing)
	}
}
