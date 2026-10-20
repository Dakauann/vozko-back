package workflow_usecase

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vozko/domain/tracepayload"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

type funcExecutor struct {
	def workflow.NodeDefinition
	run func(ctx *workflow.NodeContext) (*workflow.NodeResult, error)
}

func (e funcExecutor) Definition() workflow.NodeDefinition { return e.def }

func (e funcExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	return e.run(ctx)
}

func sharedDef(t workflow.NodeType, label string) workflow.NodeDefinition {
	return workflow.NodeDefinition{Type: t, Label: label, Scopes: []workflow.NodeScope{workflow.NodeScopeShared}}
}

func singleNodeTester(t *testing.T, node workflow.Node, edges []workflow.Edge, executors ...funcExecutor) (TestNodeUseCase, *NodeExecutorRegistry) {
	t.Helper()
	nodes := []workflow.Node{{ID: "trigger1", Type: workflow.NodeTypeTriggerManual}, node, {ID: "ok", Type: workflow.NodeTypeEnd}, {ID: "bad", Type: workflow.NodeTypeEnd}}
	if edges == nil {
		edges = []workflow.Edge{{Source: "trigger1", Target: node.ID}, {Source: node.ID, Target: "ok", Label: "sucesso"}, {Source: node.ID, Target: "bad", Label: "falha"}}
	}
	repo := NewMockWorkflowRepository()
	_ = repo.Create(createTestWorkflow("wf1", "ws1", nodes, edges))
	registry := NewNodeExecutorRegistry()
	for _, e := range executors {
		registry.Register(e.def.Type, e)
	}
	return NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Steps: registry, Executors: fixedExecutors(registry), Timeout: 2 * time.Second}), registry
}

func TestANodeTestReturnsTheProductionStepRecordWithRealMilliseconds(t *testing.T) {
	exec := funcExecutor{def: sharedDef(workflow.NodeTypeActionSetVariable, "Definir Variável"), run: func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		time.Sleep(20 * time.Millisecond)
		ctx.Trace.RecordHTTP(workflow.HTTPExchange{Method: "GET", URL: "https://api.example.com/x", Status: 200})
		ctx.Trace.RecordScriptLogs([]workflow.ScriptLog{{Level: "info", Message: "olá"}})
		return &workflow.NodeResult{Output: map[string]interface{}{"value": "pronto"}, NextNodeID: "ok"}, nil
	}}
	uc, _ := singleNodeTester(t, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSetVariable, Config: map[string]interface{}{"value": "{{nome}}"}}, nil, exec)

	out, err := uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "n1", TriggerVars: map[string]interface{}{"nome": "Ana"}})
	if err != nil || !out.Success {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	step := out.Step
	if step == nil || step.NodeID != "n1" || step.Status != workflow.LogStatusExecuted || step.Branch != "sucesso" {
		t.Fatalf("step = %+v", step)
	}
	if step.Input["value"] != "Ana" || step.Output["value"] != "pronto" {
		t.Fatalf("the step carries the parameters used and the output: %+v %+v", step.Input, step.Output)
	}
	if len(step.HTTP) != 1 || len(step.ScriptLogs) != 1 || step.StartedAt == nil || step.FinishedAt == nil {
		t.Fatalf("the step carries the trace: %+v", step)
	}
	if step.DurationMs < 20 || out.ExecutionDurationMs < 20 || out.ExecutionDurationMs > 10_000 {
		t.Fatalf("durations must be milliseconds: step %d, test %d", step.DurationMs, out.ExecutionDurationMs)
	}
	encoded, _ := json.Marshal(out)
	var wire map[string]interface{}
	_ = json.Unmarshal(encoded, &wire)
	if ms, _ := wire["execution_duration_ms"].(float64); ms < 20 || ms > 10_000 {
		t.Fatalf("execution_duration_ms on the wire = %v", wire["execution_duration_ms"])
	}
}

func TestAPanickingExecutorIsReportedWithItsGoStack(t *testing.T) {
	exec := funcExecutor{def: sharedDef(workflow.NodeTypeActionSetVariable, "Definir Variável"), run: func(*workflow.NodeContext) (*workflow.NodeResult, error) {
		var m map[string]int
		m["boom"]++
		return nil, nil
	}}
	uc, _ := singleNodeTester(t, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSetVariable}, nil, exec)
	out, err := uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "n1"})
	if err != nil || out.Success || out.ErrorKind != workflow.ErrorKindPanic {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	if !strings.Contains(out.Step.Stack, "goroutine") || out.Step.ErrorKind != workflow.ErrorKindPanic {
		t.Fatalf("the step must carry the Go stack: %+v", out.Step)
	}
}

func TestAHangingExecutorIsCutAtTheTimeLimit(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	exec := funcExecutor{def: sharedDef(workflow.NodeTypeActionSetVariable, "Definir Variável"), run: func(*workflow.NodeContext) (*workflow.NodeResult, error) {
		<-release
		return &workflow.NodeResult{}, nil
	}}
	repo := NewMockWorkflowRepository()
	_ = repo.Create(createTestWorkflow("wf1", "ws1", []workflow.Node{{ID: "trigger1", Type: workflow.NodeTypeTriggerManual}, {ID: "n1", Type: workflow.NodeTypeActionSetVariable}}, []workflow.Edge{{Source: "trigger1", Target: "n1"}}))
	registry := NewNodeExecutorRegistry()
	registry.Register(workflow.NodeTypeActionSetVariable, exec)
	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Executors: fixedExecutors(registry), Timeout: 50 * time.Millisecond})

	started := time.Now()
	out, err := uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "n1"})
	if err != nil || out.ErrorKind != workflow.ErrorKindTimeout || out.Step == nil || out.Step.ErrorKind != workflow.ErrorKindTimeout {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("the test hung for %s", time.Since(started))
	}
}

func TestAnOversizedOutputIsCappedInTheStepAndTheResponse(t *testing.T) {
	huge := strings.Repeat("x", tracepayload.MaxBytes*2)
	exec := funcExecutor{def: sharedDef(workflow.NodeTypeActionSetVariable, "Definir Variável"), run: func(*workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{Output: map[string]interface{}{"body": huge}}, nil
	}}
	uc, _ := singleNodeTester(t, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSetVariable}, nil, exec)
	out, err := uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "n1"})
	if err != nil || !out.Success {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	if out.Step.Output[tracepayload.TruncatedKey] != true || out.ExecutionOutput[tracepayload.TruncatedKey] != true || out.StateAfter[tracepayload.TruncatedKey] != true {
		t.Fatal("every payload must be capped")
	}
}

func TestTheNodeTestNeverRunsTheProductionRegistry(t *testing.T) {
	calls := 0
	live := NewNodeExecutorRegistry()
	live.Register(workflow.NodeTypeActionSetVariable, funcExecutor{def: sharedDef(workflow.NodeTypeActionSetVariable, "Definir Variável"), run: func(*workflow.NodeContext) (*workflow.NodeResult, error) {
		calls++
		return &workflow.NodeResult{}, nil
	}})
	repo := NewMockWorkflowRepository()
	_ = repo.Create(createTestWorkflow("wf1", "ws1", []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
		{ID: "n1", Type: workflow.NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "x", "value": "1"}},
	}, []workflow.Edge{{Source: "trigger1", Target: "n1"}}))
	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Steps: live})

	out, err := uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "n1"})
	if err != nil || !out.Success {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	if calls != 0 {
		t.Fatalf("the production registry ran %d times", calls)
	}
}

func TestAnExternalCallNeedsAnExplicitConfirmation(t *testing.T) {
	calls := 0
	exec := funcExecutor{def: sharedDef(workflow.NodeTypeActionHTTPRequest, "Requisição HTTP"), run: func(*workflow.NodeContext) (*workflow.NodeResult, error) {
		calls++
		return &workflow.NodeResult{Output: map[string]interface{}{"status_code": 200}}, nil
	}}
	uc, _ := singleNodeTester(t, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionHTTPRequest, Config: map[string]interface{}{"url": "https://api.example.com"}}, nil, exec)

	out, err := uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "n1"})
	if err != nil || out.Success || out.ErrorKind != workflow.ErrorKindRealCallNotConfirmed || calls != 0 {
		t.Fatalf("out = %+v, calls = %d", out, calls)
	}
	out, err = uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "n1", ConfirmRealCalls: true})
	if err != nil || !out.Success || calls != 1 {
		t.Fatalf("out = %+v, calls = %d", out, calls)
	}
}

func TestAWaitNodeDescribesTheWaitInsteadOfWaiting(t *testing.T) {
	repo := NewMockWorkflowRepository()
	_ = repo.Create(createTestWorkflow("wf1", "ws1", []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
		{ID: "timer", Type: workflow.NodeTypeWaitDuration, Config: map[string]interface{}{"seconds": float64(300)}},
		{ID: "reply", Type: workflow.NodeTypeWaitForReply, Config: map[string]interface{}{"timeout_seconds": float64(600)}},
	}, []workflow.Edge{{Source: "trigger1", Target: "timer"}, {Source: "timer", Target: "reply", Label: "completed"}}))
	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Timeout: 2 * time.Second})

	out, err := uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "timer"})
	if err != nil || !out.Success || out.Wait == nil || out.Wait.Description != "aguardaria 300 s" || out.Step.Status != workflow.LogStatusWaiting {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	out, err = uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "reply"})
	if err != nil || out.Wait == nil || out.Wait.Reason != workflow.WaitReasonReply || !strings.HasPrefix(out.Wait.Description, "aguardaria a resposta do contato") {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
}

func TestANodeOfTheOtherWorkflowTypeIsRefusedClearly(t *testing.T) {
	repo := NewMockWorkflowRepository()
	messaging := createTestWorkflow("wf-msg", "ws1", []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerManual},
		{ID: "audio", Type: workflow.NodeTypeActionPlayAudio, Config: map[string]interface{}{"media_id": "m1"}},
	}, []workflow.Edge{{Source: "trigger1", Target: "audio"}})
	messaging.Type = workflow.WorkflowTypeMessages
	_ = repo.Create(messaging)
	voice := voiceWorkflow("wf-voice", "trunk-1", "greeting")
	voice.Type = workflow.WorkflowTypeVoice
	voice.Graph.Nodes = append(voice.Graph.Nodes, workflow.Node{ID: "text", Type: workflow.NodeTypeActionSendText, Config: map[string]interface{}{"text": "oi"}})
	_ = repo.Create(voice)
	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Timeout: 2 * time.Second})

	for _, tc := range []struct{ workflowID, nodeID string }{{"wf-msg", "audio"}, {"wf-voice", "text"}} {
		out, err := uc.Execute(context.Background(), TestNodeInput{WorkflowID: tc.workflowID, WorkspaceID: "ws1", NodeID: tc.nodeID})
		if err != nil || out.Success || out.ErrorKind != workflow.ErrorKindWrongWorkflowType {
			t.Fatalf("%s/%s: out = %+v, err = %v", tc.workflowID, tc.nodeID, out, err)
		}
	}
}

func TestExecuteUntilReturnsEveryUpstreamStepAndHonoursPins(t *testing.T) {
	repo := NewMockWorkflowRepository()
	_ = repo.Create(pinnedLoginWorkflow())
	registry := NewNodeExecutorRegistry()
	registry.Register(workflow.NodeTypeActionHTTPRequest, &captureHTTPExecutor{ran: new(int)})
	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Executors: fixedExecutors(registry)})

	out, err := uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "matricula", ConfirmRealCalls: true})
	if err != nil || !out.Success {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	steps := out.UpstreamSteps
	if len(steps) < 2 || steps[len(steps)-2].NodeID != "trigger1" || steps[len(steps)-1].NodeID != "login" {
		t.Fatalf("upstream steps in order, ending with the trigger and the login: %+v", steps)
	}
	for _, s := range steps {
		if s.NodeID == "login" && (!s.Pinned || s.Branch != "sucesso") {
			t.Fatalf("the pinned upstream step = %+v", s)
		}
	}
}

func cepAgentWorkflow() *workflow.Workflow {
	wf := createTestWorkflow("wf-cep", "ws1", []workflow.Node{
		{ID: "t", Type: workflow.NodeTypeTriggerMessageReceived},
		{ID: "n2", Type: workflow.NodeTypeActionAIAgent, Config: map[string]interface{}{"source": "inline", "prompt": "Pergunte o CEP"}},
		{ID: "n3", Type: workflow.NodeTypeWaitForReply, Config: map[string]interface{}{"timeout_seconds": float64(600)}},
		{ID: "n5", Type: workflow.NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "cep", "value": "{{message}}"}},
		{ID: "n7", Type: workflow.NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "endereco", "value": "{{node.n5.value}}"}},
		{ID: "end", Type: workflow.NodeTypeEnd},
	}, []workflow.Edge{
		{Source: "t", Target: "n2"},
		{Source: "n2", Target: "n3"},
		{Source: "n2", Target: "n5", Label: "buscar_cep"},
		{Source: "n3", Target: "n2", Label: "replied"},
		{Source: "n3", Target: "end", Label: "timeout"},
		{Source: "n5", Target: "n7"},
		{Source: "n7", Target: "end"},
	})
	wf.Type = workflow.WorkflowTypeMessages
	return wf
}

func cepAgentTester(t *testing.T) TestNodeUseCase {
	t.Helper()
	repo := NewMockWorkflowRepository()
	_ = repo.Create(cepAgentWorkflow())
	registry := NewNodeExecutorRegistry()
	registry.Register(workflow.NodeTypeActionAIAgent, funcExecutor{
		def: workflow.NodeDefinition{Type: workflow.NodeTypeActionAIAgent, Label: "Agente IA", Scopes: []workflow.NodeScope{workflow.NodeScopeWhatsApp}},
		run: func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
			if strings.Contains(ctx.State.GetString("message"), "01001000") {
				return &workflow.NodeResult{Output: map[string]interface{}{"tool": "buscar_cep"}, NextNodeID: "n5"}, nil
			}
			return &workflow.NodeResult{Output: map[string]interface{}{"response": "Qual é o seu CEP?"}, NextNodeID: "n3"}, nil
		},
	})
	registry.Register(workflow.NodeTypeWaitForReply, node_executors.NewWaitForReplyExecutor())
	registry.Register(workflow.NodeTypeActionSetVariable, node_executors.NewSetVariableExecutor())
	return NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Executors: fixedExecutors(registry), Timeout: 2 * time.Second})
}

func TestAnAgentThatTookItsDefaultEdgeIntoAWaitExplainsWhyTheTargetWasNotReached(t *testing.T) {
	out, err := cepAgentTester(t).Execute(context.Background(), TestNodeInput{WorkflowID: "wf-cep", WorkspaceID: "ws1", NodeID: "n7", TriggerVars: map[string]interface{}{"message": "oi"}})
	if err != nil || out.Success || out.ErrorKind != workflow.ErrorKindTargetNotReached {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	if len(out.UpstreamSteps) != 3 || out.UpstreamSteps[1].NodeID != "n2" || out.UpstreamSteps[1].Branch != "" || out.UpstreamSteps[2].NodeID != "n3" || out.UpstreamSteps[2].Status != workflow.LogStatusWaiting {
		t.Fatalf("every upstream step with its record: %+v", out.UpstreamSteps)
	}
	stop := out.PathStop
	if stop == nil || stop.Reason != PathStopWaits || stop.StoppedAt != "n3" || stop.Wait == nil || stop.Wait.Reason != workflow.WaitReasonReply {
		t.Fatalf("stop = %+v", stop)
	}
	if stop.DivergedAt != "n2" || stop.TakenBranch != "" || stop.ExpectedBranch != "buscar_cep" {
		t.Fatalf("the divergence must name the agent and both branches: %+v", stop)
	}
	want := "O caminho até n7 parou em n3 (Aguardar Resposta): n2 (Agente IA) seguiu a saída padrão, não buscar_cep."
	if stop.Message != want || !strings.HasPrefix(out.Error, want) {
		t.Fatalf("message = %q", stop.Message)
	}
	fixes := map[string]NodeTestFix{}
	for _, f := range stop.Fixes {
		fixes[f.Kind] = f
	}
	if f := fixes[FixPinEdge]; f.NodeID != "n2" || f.Edge != "buscar_cep" {
		t.Fatalf("pin fix = %+v", f)
	}
	if f := fixes[FixContinueWait]; f.NodeID != "n3" || f.Needs != "reply" {
		t.Fatalf("wait fix = %+v", f)
	}
	if f := fixes[FixMockAndRunDirect]; f.NodeID != "n7" {
		t.Fatalf("direct fix = %+v", f)
	}
}

func TestAnsweringTheUpstreamWaitLetsTheTestReachTheTarget(t *testing.T) {
	out, err := cepAgentTester(t).Execute(context.Background(), TestNodeInput{
		WorkflowID: "wf-cep", WorkspaceID: "ws1", NodeID: "n7", TriggerVars: map[string]interface{}{"message": "oi"},
		ContinueWaits: true, WaitAnswers: map[string]TestWaitAnswer{"n3": {Reply: "meu cep é 01001000"}},
	})
	if err != nil || !out.Success {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	var path []string
	for _, s := range out.UpstreamSteps {
		path = append(path, s.NodeID)
	}
	if strings.Join(path, ",") != "t,n2,n3,n2,n5" {
		t.Fatalf("path = %v", path)
	}
	if out.ExecutionOutput["value"] == nil && out.StateAfter["endereco"] == nil {
		t.Fatalf("the target ran on the answered path: %+v", out)
	}
}

func TestContinuingWaitsWithoutAnAnswerStillStopsAndSaysWhatIsMissing(t *testing.T) {
	out, err := cepAgentTester(t).Execute(context.Background(), TestNodeInput{
		WorkflowID: "wf-cep", WorkspaceID: "ws1", NodeID: "n7", TriggerVars: map[string]interface{}{"message": "oi"}, ContinueWaits: true,
	})
	if err != nil || out.PathStop == nil || out.PathStop.Reason != PathStopWaits || out.PathStop.StoppedAt != "n3" {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
}
