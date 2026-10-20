package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/redact"
	"vozko/domain/tracepayload"
	"vozko/domain/workflow"
)

type traceExecutor struct {
	nodeType workflow.NodeType
	run      func(ctx *workflow.NodeContext) (*workflow.NodeResult, error)
}

func (e traceExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{Type: e.nodeType}
}

func (e traceExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	return e.run(ctx)
}

type metricsSpy struct {
	mu         sync.Mutex
	started    []string
	finished   []string
	stepTypes  []string
	deliveries []string

	lookupFailures int
	continuations  []string
}

func (m *metricsSpy) IncWorkflowRunStarted(trigger string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = append(m.started, trigger)
}

func (m *metricsSpy) IncWorkflowRunFinished(status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.finished = append(m.finished, status)
}

func (m *metricsSpy) ObserveWorkflowStepDuration(nodeType string, _ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stepTypes = append(m.stepTypes, nodeType)
}

func (m *metricsSpy) IncWorkflowWebhookDelivery(outcome, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deliveries = append(m.deliveries, outcome+"/"+reason)
}

func (m *metricsSpy) IncWorkflowReplyLookupFailed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lookupFailures++
}

func (m *metricsSpy) IncWorkflowAgentContinuation(outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.continuations = append(m.continuations, outcome)
}

type traceHarness struct {
	registry *NodeExecutorRegistry
	runs     *MockWorkflowRunRepository
	logs     *MockWorkflowRunLogRepository
	metrics  *metricsSpy
	engine   *RunEngine
}

func newTraceHarness() *traceHarness {
	h := &traceHarness{
		registry: NewNodeExecutorRegistry(),
		runs:     NewMockWorkflowRunRepository(),
		logs:     NewMockWorkflowRunLogRepository(),
		metrics:  &metricsSpy{},
	}
	h.engine = NewRunEngine(h.runs, h.logs, h.registry)
	h.engine.SetMetrics(h.metrics)
	return h
}

func (h *traceHarness) register(nodeType workflow.NodeType, run func(ctx *workflow.NodeContext) (*workflow.NodeResult, error)) {
	h.registry.Register(nodeType, traceExecutor{nodeType: nodeType, run: run})
}

func (h *traceHarness) start(w *workflow.Workflow) *workflow.WorkflowRun {
	return h.startWith(w, nil)
}

func (h *traceHarness) startWith(w *workflow.Workflow, prepare func(run *workflow.WorkflowRun)) *workflow.WorkflowRun {
	run := &workflow.WorkflowRun{
		ID: "run-1", WorkflowID: w.ID, WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp",
		Status: workflow.RunStatusRunning, CurrentNodeID: w.Graph.Nodes[0].ID, TriggerNodeID: w.Graph.Nodes[0].ID,
		TriggerType: workflow.TriggerWebhook, State: workflow.NewRunState(),
	}
	if prepare != nil {
		prepare(run)
	}
	_ = h.runs.Create(run)
	if err := h.engine.Execute(run, w); err != nil {
		panic(err)
	}
	return run
}

func (h *traceHarness) step(t *testing.T, nodeID string) *workflow.WorkflowRunLog {
	t.Helper()
	entries, _ := h.logs.FindByRunID("run-1")
	for _, e := range entries {
		if e.NodeID == nodeID {
			return e
		}
	}
	t.Fatalf("no step row for node %s in %+v", nodeID, entries)
	return nil
}

func linearWorkflow(nodes ...workflow.Node) *workflow.Workflow {
	w := &workflow.Workflow{ID: "wf-1", WorkspaceID: "ws-1", Type: workflow.WorkflowTypeMessages}
	w.Graph.Nodes = append([]workflow.Node{{ID: "t1", Type: workflow.NodeTypeTriggerWebhook}}, nodes...)
	w.Graph.Nodes = append(w.Graph.Nodes, workflow.Node{ID: "end", Type: workflow.NodeTypeEnd})
	for i := 0; i < len(w.Graph.Nodes)-1; i++ {
		w.Graph.Edges = append(w.Graph.Edges, workflow.Edge{Source: w.Graph.Nodes[i].ID, Target: w.Graph.Nodes[i+1].ID})
	}
	return w
}

func TestAnExecutedStepRecordsTheParametersActuallyUsedWithSecretsLeftAsPlaceholders(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{Output: map[string]interface{}{"status_code": 200}}, nil
	})
	w := linearWorkflow(workflow.Node{ID: "n55", Type: workflow.NodeTypeActionHTTPRequest, Config: map[string]interface{}{
		"url":        "https://api.exemplo.com/alunos/{{cpf}}",
		"auth_token": "{{secret.unifecaf_senha}}",
		"headers":    map[string]interface{}{"X-Curso": "{{curso}}"},
	}})
	h.startWith(w, func(run *workflow.WorkflowRun) {
		run.State.Set("cpf", "52998224725")
		run.State.Set("curso", "ADS")
	})

	step := h.step(t, "n55")
	if step.Status != workflow.LogStatusExecuted || step.Attempt != 1 {
		t.Fatalf("status=%s attempt=%d", step.Status, step.Attempt)
	}
	if step.StartedAt == nil || step.FinishedAt == nil || step.FinishedAt.Before(*step.StartedAt) || step.DurationMs < 0 {
		t.Fatalf("engine clock missing: started=%v finished=%v duration=%d", step.StartedAt, step.FinishedAt, step.DurationMs)
	}
	if step.Input["url"] != "https://api.exemplo.com/alunos/•••••••••25" {
		t.Fatalf("resolved url = %v, want the interpolated value with the CPF masked", step.Input["url"])
	}
	if step.Input["auth_token"] != redact.Mask {
		t.Fatalf("a credential-named parameter must be masked, got %v", step.Input["auth_token"])
	}
	headers, _ := step.Input["headers"].(map[string]interface{})
	if headers["X-Curso"] != "ADS" {
		t.Fatalf("nested parameters are resolved too, got %v", step.Input["headers"])
	}
}

func TestSecretPlaceholdersSurviveInTheRecordedParameters(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{Output: map[string]interface{}{}}, nil
	})
	h.start(linearWorkflow(workflow.Node{ID: "n55", Type: workflow.NodeTypeActionHTTPRequest, Config: map[string]interface{}{
		"body": `{"login":"{{secret.unifecaf_login}}"}`,
	}}))
	if body := h.step(t, "n55").Input["body"]; body != `{"login":"{{secret.unifecaf_login}}"}` {
		t.Fatalf("body = %v, want the placeholder kept", body)
	}
}

func TestTheCodeNodeRecordsItsCodeAsWrittenBecauseCodeIsNeverInterpolated(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionCode, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{Output: map[string]interface{}{"ok": true}}, nil
	})
	code := "return { s: '{{node.n55.status_code}}' }"
	h.start(linearWorkflow(workflow.Node{ID: "n56", Type: workflow.NodeTypeActionCode, Config: map[string]interface{}{"code": code}}))
	if got := h.step(t, "n56").Input["code"]; got != code {
		t.Fatalf("code = %v", got)
	}
}

func TestTheBranchIsTheLabelOfTheEdgeTheNodeChose(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{NextNodeID: "bad", Output: map[string]interface{}{"next_edge": "falha"}}, nil
	})
	w := &workflow.Workflow{ID: "wf-1", WorkspaceID: "ws-1", Graph: workflow.Graph{
		Nodes: []workflow.Node{
			{ID: "t1", Type: workflow.NodeTypeTriggerWebhook},
			{ID: "n55", Type: workflow.NodeTypeActionHTTPRequest},
			{ID: "ok", Type: workflow.NodeTypeEnd},
			{ID: "bad", Type: workflow.NodeTypeEnd},
		},
		Edges: []workflow.Edge{
			{Source: "t1", Target: "n55"},
			{Source: "n55", Target: "ok", Label: "sucesso"},
			{Source: "n55", Target: "bad", Label: "falha"},
		},
	}}
	h.start(w)
	if branch := h.step(t, "n55").Branch; branch != "falha" {
		t.Fatalf("branch = %q, want falha", branch)
	}
}

func TestAFailedAttemptRecordsTheKindTheChainAndTheAttemptNumber(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return nil, fmt.Errorf("n55 login: %w", fmt.Errorf("dial: %w", context.DeadlineExceeded))
	})
	run := h.start(linearWorkflow(workflow.Node{ID: "n55", Type: workflow.NodeTypeActionHTTPRequest}))
	if run.Status != workflow.RunStatusWaiting || run.WaitReason != workflow.WaitReasonRetry {
		t.Fatalf("run = %s/%s, want a retry wait", run.Status, run.WaitReason)
	}
	step := h.step(t, "n55")
	if step.Status != workflow.LogStatusFailed || step.ErrorKind != workflow.ErrorKindTimeout || step.Attempt != 1 {
		t.Fatalf("step = %+v", step)
	}
	if len(step.ErrorChain) != 3 || step.ErrorChain[2] != "context deadline exceeded" {
		t.Fatalf("chain = %q", step.ErrorChain)
	}

	run.Status = workflow.RunStatusRunning
	if err := h.engine.Execute(run, linearWorkflow(workflow.Node{ID: "n55", Type: workflow.NodeTypeActionHTTPRequest})); err != nil {
		t.Fatal(err)
	}
	entries, _ := h.logs.FindByRunID("run-1")
	last := entries[len(entries)-1]
	if last.NodeID != "n55" || last.Attempt != 2 {
		t.Fatalf("second attempt = %+v", last)
	}
}

func TestAnExecutorDeclaredFailureKindIsKept(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionCode, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		ctx.Trace.RecordStack("Error: boom\n\tat (3:5)")
		ctx.Trace.RecordScriptLogs([]workflow.ScriptLog{{Level: "info", Message: "etapa login"}})
		return nil, workflow.Fail(workflow.ErrorKindScript, errors.New("action_code: boom"))
	})
	h.start(linearWorkflow(workflow.Node{ID: "n56", Type: workflow.NodeTypeActionCode}))
	step := h.step(t, "n56")
	if step.ErrorKind != workflow.ErrorKindScript || !strings.Contains(step.Stack, "3:5") {
		t.Fatalf("step = %+v", step)
	}
	if len(step.ScriptLogs) != 1 || step.ScriptLogs[0].Message != "etapa login" {
		t.Fatalf("script logs = %+v", step.ScriptLogs)
	}
}

func TestAPanickingNodeRecordsTheGoStack(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionCode, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		var m map[string]int
		m["x"]++
		return nil, nil
	})
	run := h.start(linearWorkflow(workflow.Node{ID: "n56", Type: workflow.NodeTypeActionCode}))
	step := h.step(t, "n56")
	if run.Status != workflow.RunStatusError || step.ErrorKind != workflow.ErrorKindPanic {
		t.Fatalf("run=%s step=%+v", run.Status, step)
	}
	if !strings.Contains(step.Stack, "goroutine") || !strings.Contains(step.Stack, "run_engine") {
		t.Fatalf("stack = %q, want the Go stack from the recovery", step.Stack)
	}
}

func TestAResultErrorWithoutAKindIsAValidationFailure(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionCode, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{Error: "agent not found: a1"}, nil
	})
	h.start(linearWorkflow(workflow.Node{ID: "n1", Type: workflow.NodeTypeActionCode}))
	step := h.step(t, "n1")
	if step.ErrorKind != workflow.ErrorKindValidation || len(step.ErrorChain) != 1 {
		t.Fatalf("step = %+v", step)
	}
	h2 := newTraceHarness()
	h2.register(workflow.NodeTypeActionCode, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{Error: "missing secret x", ErrorKind: workflow.ErrorKindConfig}, nil
	})
	h2.start(linearWorkflow(workflow.Node{ID: "n1", Type: workflow.NodeTypeActionCode}))
	if kind := h2.step(t, "n1").ErrorKind; kind != workflow.ErrorKindConfig {
		t.Fatalf("declared kind = %q", kind)
	}
}

func TestASkippedNodeIsRecordedAsSkipped(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionSendText, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{Skipped: true, Output: map[string]interface{}{"skipped_reason": "unsupported_on_channel"}}, nil
	})
	run := h.start(linearWorkflow(workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSendText}))
	if run.Status != workflow.RunStatusCompleted {
		t.Fatalf("a skipped node lets the run continue, got %s", run.Status)
	}
	if step := h.step(t, "n1"); step.Status != workflow.LogStatusSkipped {
		t.Fatalf("status = %s", step.Status)
	}
}

func TestAWaitingNodeIsRecordedAsWaitingAndAFailedWakeEnqueueLeavesARow(t *testing.T) {
	h := newTraceHarness()
	h.engine.SetWakeScheduler(failingWakeScheduler{})
	h.register(workflow.NodeTypeWaitDuration, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{Wait: &workflow.WaitInstruction{WakeAt: 123, Reason: workflow.WaitReasonDuration}}, nil
	})
	h.start(linearWorkflow(workflow.Node{ID: "w1", Type: workflow.NodeTypeWaitDuration}))
	entries, _ := h.logs.FindByRunID("run-1")
	var waiting, enqueue *workflow.WorkflowRunLog
	for _, e := range entries {
		if e.NodeID != "w1" {
			continue
		}
		if e.Error == "" {
			waiting = e
		} else {
			enqueue = e
		}
	}
	if waiting == nil || waiting.Status != workflow.LogStatusWaiting {
		t.Fatalf("waiting row = %+v", waiting)
	}
	if enqueue == nil || enqueue.ErrorKind != workflow.ErrorKindTransport || !strings.Contains(enqueue.Error, "queue down") {
		t.Fatalf("enqueue failure row = %+v", enqueue)
	}
}

type failingWakeScheduler struct{}

func (failingWakeScheduler) ScheduleRunWake(string, int64) error { return errors.New("queue down") }

func TestGuardStopsLeaveARowWithTheReason(t *testing.T) {
	cases := []struct {
		name    string
		w       *workflow.Workflow
		prepare func(run *workflow.WorkflowRun)
		node    string
		want    string
	}{
		{
			name: "node not found",
			w:    linearWorkflow(),
			prepare: func(run *workflow.WorkflowRun) {
				run.CurrentNodeID = "ghost"
			},
			node: "ghost", want: "node not found",
		},
		{
			name: "no executor",
			w:    linearWorkflow(workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSendEmail}),
			node: "n1", want: workflow.ErrNodeExecutorNotFound.Error(),
		},
		{
			name: "durable steps exhausted",
			w:    linearWorkflow(workflow.Node{ID: "n1", Type: workflow.NodeTypeActionCode}),
			prepare: func(run *workflow.WorkflowRun) {
				run.State.Set(workflow.StateKeyDurableSteps, workflow.MaxDurableExecutionsPerRun)
			},
			node: "t1", want: workflow.ErrDurableExecutionsReached.Error(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTraceHarness()
			h.register(workflow.NodeTypeActionCode, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
				return &workflow.NodeResult{}, nil
			})
			run := &workflow.WorkflowRun{
				ID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "e", EntryType: "whatsapp",
				Status: workflow.RunStatusRunning, CurrentNodeID: "t1", State: workflow.NewRunState(),
			}
			if tc.prepare != nil {
				tc.prepare(run)
			}
			_ = h.runs.Create(run)
			if err := h.engine.Execute(run, tc.w); err != nil {
				t.Fatal(err)
			}
			step := h.step(t, tc.node)
			if step.Status != workflow.LogStatusFailed || step.ErrorKind != workflow.ErrorKindGuard || !strings.Contains(step.Error, tc.want) {
				t.Fatalf("guard row = %+v", step)
			}
		})
	}
}

func TestALoopStopLeavesAGuardRow(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionCode, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{}, nil
	})
	w := &workflow.Workflow{ID: "wf-1", WorkspaceID: "ws-1", Graph: workflow.Graph{
		Nodes: []workflow.Node{{ID: "t1", Type: workflow.NodeTypeTriggerWebhook}, {ID: "a", Type: workflow.NodeTypeActionCode}, {ID: "b", Type: workflow.NodeTypeActionCode}},
		Edges: []workflow.Edge{{Source: "t1", Target: "a"}, {Source: "a", Target: "b"}, {Source: "b", Target: "a"}},
	}}
	h.start(w)
	entries, _ := h.logs.FindByRunID("run-1")
	last := entries[len(entries)-1]
	if last.ErrorKind != workflow.ErrorKindGuard || !strings.Contains(last.Error, "loop detected") {
		t.Fatalf("last row = %+v", last)
	}
}

func TestAnOversizedOutputIsCappedWithATruncatedMarker(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{Output: map[string]interface{}{"body": strings.Repeat("z", 3*tracepayload.MaxBytes)}}, nil
	})
	h.start(linearWorkflow(workflow.Node{ID: "n55", Type: workflow.NodeTypeActionHTTPRequest}))
	if h.step(t, "n55").Output[tracepayload.TruncatedKey] != true {
		t.Fatal("the stored output must be capped and say so")
	}
}

func TestTheEngineCountsStartsFinishesAndStepDurations(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionCode, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{}, nil
	})
	h.start(linearWorkflow(workflow.Node{ID: "n1", Type: workflow.NodeTypeActionCode}))
	if strings.Join(h.metrics.started, ",") != string(workflow.TriggerWebhook) {
		t.Fatalf("started = %v", h.metrics.started)
	}
	if strings.Join(h.metrics.finished, ",") != string(workflow.RunStatusCompleted) {
		t.Fatalf("finished = %v", h.metrics.finished)
	}
	if strings.Join(h.metrics.stepTypes, ",") != string(workflow.NodeTypeActionCode) {
		t.Fatalf("step durations = %v", h.metrics.stepTypes)
	}
}

func TestAGateCancelLeavesAGateRow(t *testing.T) {
	logs := NewMockWorkflowRunLogRepository()
	engine := NewRunEngine(nil, logs, nil)
	engine.SetAutomationGate(&gateStub{enabled: false})
	run := parkedRun()
	if err := resumeRunFromCurrent(&workflowRepoStub{t: t}, &runRepoStub{}, engine, run); err != nil {
		t.Fatal(err)
	}
	entries, _ := logs.FindByRunID(run.ID)
	if len(entries) != 1 || entries[0].Status != workflow.LogStatusGate || entries[0].ErrorKind != workflow.ErrorKindGate || entries[0].NodeID != "node-7" {
		t.Fatalf("gate rows = %+v", entries)
	}
}

func TestAnUnreadableGateLeavesAGateRowPerAttempt(t *testing.T) {
	logs := NewMockWorkflowRunLogRepository()
	engine := NewRunEngine(nil, logs, nil)
	engine.SetAutomationGate(&gateStub{err: errors.New("db down")})
	run := parkedRun()
	run.State = workflow.NewRunState()
	if err := resumeRunFromCurrent(&workflowRepoStub{t: t}, &runRepoStub{}, engine, run); err != nil {
		t.Fatal(err)
	}
	entries, _ := logs.FindByRunID(run.ID)
	if len(entries) != 1 || entries[0].Status != workflow.LogStatusGate || entries[0].Attempt != 1 || !strings.Contains(entries[0].Error, "db down") {
		t.Fatalf("gate rows = %+v", entries)
	}
}

func TestGetRunReturnsTheNewStepFieldsRedacted(t *testing.T) {
	runs, _ := runOwnedBy("ws-owner")
	logs := NewMockWorkflowRunLogRepository()
	_ = logs.Create(&workflow.WorkflowRunLog{
		ID: "l1", RunID: "run-1", NodeID: "n55", Status: workflow.LogStatusFailed, Attempt: 2, Branch: "falha",
		ErrorKind:  workflow.ErrorKindHTTPStatus,
		ErrorChain: []string{"login: Bearer tok-1 refused"},
		Stack:      `Error: {"password":"p-1"}`,
		ScriptLogs: []workflow.ScriptLog{{Level: "info", Message: "cpf 529.982.247-25"}},
		HTTP: []workflow.HTTPExchange{{
			Method: "POST", URL: "https://api.exemplo.com/login?access_token=abc&curso=ADS",
			RequestHeaders: map[string]string{"Authorization": "Bearer tok-1", "X-Curso": "ADS"},
			RequestBody:    `{"senha":"p-1"}`, Status: 500,
		}},
	})

	detail, err := NewGetRunUseCase(runs, logs, seesEverything{}).Execute(viewer, "ws-owner", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	step := detail.Logs[0]
	if step.Attempt != 2 || step.Branch != "falha" || step.ErrorKind != workflow.ErrorKindHTTPStatus {
		t.Fatalf("step fields lost: %+v", step)
	}
	x := step.HTTP[0]
	if x.RequestHeaders["Authorization"] != redact.Mask || x.RequestHeaders["X-Curso"] != "ADS" {
		t.Fatalf("headers = %v", x.RequestHeaders)
	}
	if strings.Contains(x.URL, "abc") || !strings.Contains(x.URL, "curso=ADS") || strings.Contains(x.RequestBody, "p-1") {
		t.Fatalf("exchange = %+v", x)
	}
	if strings.Contains(step.ErrorChain[0], "tok-1") || strings.Contains(step.Stack, "p-1") || strings.Contains(step.ScriptLogs[0].Message, "982") {
		t.Fatalf("chain=%q stack=%q logs=%+v", step.ErrorChain, step.Stack, step.ScriptLogs)
	}
}

func TestSecretValuesNeverReachTheStoredStep(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		ctx.Trace.ScrubWith(redact.SecretsOnly("TOPSECRET"))
		ctx.Trace.RecordHTTP(workflow.HTTPExchange{Method: "GET", URL: "https://a/x?q=TOPSECRET", RequestHeaders: map[string]string{"X-Plain": "TOPSECRET"}})
		return nil, fmt.Errorf("upstream echoed %s", "TOPSECRET")
	})
	h.start(linearWorkflow(workflow.Node{ID: "n55", Type: workflow.NodeTypeActionHTTPRequest}))
	step := h.step(t, "n55")
	stored := fmt.Sprintf("%+v", *step)
	if strings.Contains(stored, "TOPSECRET") {
		t.Fatalf("a secret value reached the step row: %s", stored)
	}
}
