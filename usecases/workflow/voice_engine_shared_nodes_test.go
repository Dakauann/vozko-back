package workflow_usecase

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"

	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

type failingStep struct {
	calls int
}

func (s *failingStep) Execute(*workflow.NodeContext) (*workflow.NodeResult, error) {
	s.calls++
	return nil, errors.New("api unreachable")
}

func (s *failingStep) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{Type: workflow.NodeTypeActionHTTPRequest}
}

type pausingStep struct{}

func (pausingStep) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{Type: workflow.NodeTypeActionHTTPRequest}
}

func (pausingStep) Execute(*workflow.NodeContext) (*workflow.NodeResult, error) {
	return &workflow.NodeResult{Wait: &workflow.WaitInstruction{
		WakeAt: time.Now().Add(time.Hour).UnixMilli(),
		Reason: workflow.WaitReasonDuration,
	}}, nil
}

func callStepWorkflow() *workflow.Workflow {
	return &workflow.Workflow{
		ID:          "wf-1",
		WorkspaceID: "ws-1",
		Type:        workflow.WorkflowTypeVoice,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "t", Type: workflow.NodeTypeTriggerCallReceived},
				{ID: "step", Type: workflow.NodeTypeActionHTTPRequest},
				{ID: "end", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{{Source: "t", Target: "step"}, {Source: "step", Target: "end"}},
		},
	}
}

func runCallStep(t *testing.T, step workflow.NodeExecutor, runtime interface{}) *workflow.WorkflowRun {
	t.Helper()
	runs := NewMockWorkflowRunRepository()
	registry := NewNodeExecutorRegistry()
	registry.Register(workflow.NodeTypeActionHTTPRequest, step)
	w := callStepWorkflow()
	run := voiceRun(w)
	_ = runs.Create(run)
	if err := NewRunEngine(runs, NewMockWorkflowRunLogRepository(), registry).ExecuteWithRuntime(run, w, runtime); err != nil {
		t.Fatalf("ExecuteWithRuntime: %v", err)
	}
	return run
}

func TestAFailingStepDuringACallEndsTheRunInsteadOfRetryingWithoutTheCaller(t *testing.T) {
	step := &failingStep{}
	run := runCallStep(t, step, &scriptedCaller{})

	if run.Status != workflow.RunStatusError || run.RetryCount != 0 {
		t.Fatalf("status = %s retries = %d, want error with no retry", run.Status, run.RetryCount)
	}
	if step.calls != 1 {
		t.Fatalf("step ran %d times, want 1", step.calls)
	}
}

func TestAFailingStepOutsideACallStillRetries(t *testing.T) {
	run := runCallStep(t, &failingStep{}, nil)

	if run.Status != workflow.RunStatusWaiting || run.WaitReason != workflow.WaitReasonRetry {
		t.Fatalf("status = %s reason = %s, want a scheduled retry", run.Status, run.WaitReason)
	}
}

func TestAStepThatWouldPauseDuringACallEndsTheRun(t *testing.T) {
	run := runCallStep(t, pausingStep{}, &scriptedCaller{})

	if run.Status != workflow.RunStatusError || run.Error != workflow.ErrWaitDuringCall.Error() {
		t.Fatalf("status = %s error = %q, want ErrWaitDuringCall", run.Status, run.Error)
	}
}

func TestAHangUpStopsTheFlowBeforeTheNextStep(t *testing.T) {
	step := &failingStep{}
	caller := &scriptedCaller{ended: make(chan struct{})}
	close(caller.ended)

	run := runCallStep(t, step, caller)

	if run.Status != workflow.RunStatusCancelled {
		t.Fatalf("status = %s, want cancelled", run.Status)
	}
	if step.calls != 0 {
		t.Fatalf("a step ran %d times after the caller hung up", step.calls)
	}
}

func TestVoiceWorkflowsOfferOnlyNodesThatFinishWhileTheCallerWaits(t *testing.T) {
	registry := NewNodeExecutorRegistry()
	RegisterDefaultExecutors(registry, ExecutorDeps{})

	var offered []string
	for _, def := range registry.Catalog() {
		if def.Type.IsTrigger() || !workflow.DefinitionAllowedForType(def, workflow.WorkflowTypeVoice) {
			continue
		}
		offered = append(offered, string(def.Type))
		if def.Type.IsWait() || def.Type.ParksForReply() {
			t.Errorf("%s pauses the run and would drop the live call", def.Type)
		}
	}
	sort.Strings(offered)

	want := []string{
		string(workflow.NodeTypeActionAddSheetRow),
		string(workflow.NodeTypeActionCode),
		string(workflow.NodeTypeActionFormatDate),
		string(workflow.NodeTypeActionGetCurrentTime),
		string(workflow.NodeTypeActionHTTPRequest),
		string(workflow.NodeTypeActionLoop),
		string(workflow.NodeTypeActionPlayAudio),
		string(workflow.NodeTypeActionSendEmail),
		string(workflow.NodeTypeActionSetVariable),
		string(workflow.NodeTypeTransferToQueue),
		string(workflow.NodeTypeConditionBranch),
		string(workflow.NodeTypeConditionFilter),
		string(workflow.NodeTypeConditionTextMatch),
		string(workflow.NodeTypeEnd),
		string(workflow.NodeTypeWaitDTMF),
	}
	sort.Strings(want)
	if !reflect.DeepEqual(offered, want) {
		t.Fatalf("voice nodes = %v\nwant %v", offered, want)
	}
}

func TestSharedNodesOpenedToVoiceStayAvailableToMessaging(t *testing.T) {
	registry := NewNodeExecutorRegistry()
	RegisterDefaultExecutors(registry, ExecutorDeps{})

	for _, def := range registry.Catalog() {
		if def.Type.IsTrigger() || def.Type == workflow.NodeTypeEnd || !workflow.DefinitionAllowedForType(def, workflow.WorkflowTypeVoice) {
			continue
		}
		voiceOnly := def.Type == workflow.NodeTypeActionPlayAudio || def.Type == workflow.NodeTypeWaitDTMF || def.Type == workflow.NodeTypeTransferToQueue
		if voiceOnly == workflow.DefinitionAllowedForType(def, workflow.WorkflowTypeMessages) {
			t.Errorf("%s: voice-only=%v but messaging allowed=%v", def.Type, voiceOnly, !voiceOnly)
		}
	}
}

func TestAVoiceMenuCanBranchOnAnAPIAnswerAboutTheCaller(t *testing.T) {
	var askedFor string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		askedFor = r.URL.Query().Get("phone")
		_, _ = w.Write([]byte(`{"vip":true}`))
	}))
	defer api.Close()

	flow := &workflow.Workflow{
		ID:          "wf-1",
		WorkspaceID: "ws-1",
		Type:        workflow.WorkflowTypeVoice,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "t", Type: workflow.NodeTypeTriggerCallReceived, Config: map[string]interface{}{"trunk_id": "trunk-1"}},
				{ID: "lookup", Type: workflow.NodeTypeActionHTTPRequest, Config: map[string]interface{}{
					"method": "GET", "url": api.URL + "/clientes?phone={{caller_number}}",
				}},
				{ID: "vip_check", Type: workflow.NodeTypeConditionBranch, Config: map[string]interface{}{
					"variable": "{{node.lookup.vip}}", "operator": "is_true",
				}},
				{ID: "vip", Type: workflow.NodeTypeActionPlayAudio, Config: map[string]interface{}{"media_id": "vip"}},
				{ID: "regular", Type: workflow.NodeTypeActionPlayAudio, Config: map[string]interface{}{"media_id": "regular"}},
				{ID: "end", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{
				{Source: "t", Target: "lookup"},
				{Source: "lookup", Target: "vip_check", Label: "sucesso"},
				{Source: "lookup", Target: "regular", Label: "falha"},
				{Source: "vip_check", Target: "vip", Label: "true"},
				{Source: "vip_check", Target: "regular", Label: "false"},
				{Source: "vip", Target: "end"},
				{Source: "regular", Target: "end"},
			},
		},
	}

	registry := NewNodeExecutorRegistry()
	RegisterDefaultExecutors(registry, ExecutorDeps{
		VoiceAudio: silentAudio{},
		HTTPEgress: node_executors.HTTPEgress{Client: &http.Client{}},
	})
	if err := workflow.ValidateGraph(&flow.Graph, workflow.WorkflowTypeVoice); err != nil {
		t.Fatalf("ValidateGraph: %v", err)
	}
	if err := workflow.ValidateNodeScopes(&flow.Graph, workflow.WorkflowTypeVoice, registry.Catalog()); err != nil {
		t.Fatalf("ValidateNodeScopes: %v", err)
	}

	caller := &scriptedCaller{}
	run := newVoiceRun(flow, &flow.Graph.Nodes[0], workflow.InboundVoiceCall{
		WorkspaceID: "ws-1", CallID: "call-1", CallerNumber: "5511988887777", Call: caller,
	})
	runs := NewMockWorkflowRunRepository()
	_ = runs.Create(run)
	if err := NewRunEngine(runs, NewMockWorkflowRunLogRepository(), registry).ExecuteWithRuntime(run, flow, caller); err != nil {
		t.Fatalf("ExecuteWithRuntime: %v", err)
	}

	if run.Status != workflow.RunStatusCompleted {
		t.Fatalf("status = %s (%s), want completed", run.Status, run.Error)
	}
	if askedFor != "5511988887777" {
		t.Fatalf("the API was asked about %q, want the caller's number", askedFor)
	}
	if prev, _ := run.State.Get("_prev_node_id"); prev != "vip" || caller.plays != 1 {
		t.Fatalf("last step = %v plays = %d, want the VIP audio once", prev, caller.plays)
	}
}

func TestAVoiceWorkflowWithAnAPILookupActivatesButAPauseDoesNot(t *testing.T) {
	registry := NewNodeExecutorRegistry()
	RegisterDefaultExecutors(registry, ExecutorDeps{})
	withStep := func(id, trunkID string, step workflow.Node) *workflow.Workflow {
		w := voiceWorkflow(id, trunkID, "greeting")
		step.ID = "step"
		w.Graph.Nodes = append(w.Graph.Nodes, step)
		for i := range w.Graph.Edges {
			if w.Graph.Edges[i].Source == "t" {
				w.Graph.Edges[i].Target = "step"
			}
		}
		w.Graph.Edges = append(w.Graph.Edges, workflow.Edge{Source: "step", Target: "menu", Label: "sucesso"})
		return w
	}

	repo := NewMockWorkflowRepository()
	_ = repo.Create(withStep("wf-api", "trunk-1", workflow.Node{Type: workflow.NodeTypeActionHTTPRequest, Config: map[string]interface{}{
		"method": "GET", "url": "https://crm.example.com/clientes?phone={{caller_number}}",
	}}))
	_ = repo.Create(withStep("wf-pause", "trunk-2", workflow.Node{Type: workflow.NodeTypeWaitDuration, Config: map[string]interface{}{
		"duration": float64(1), "unit": "minutes",
	}}))
	uc := voiceActivation(repo, inboundTrunksStub{"ws1|trunk-1": true, "ws1|trunk-2": true})
	uc.SetCatalogFn(registry.Catalog)

	if _, err := uc.Execute(context.Background(), "wf-api"); err != nil {
		t.Fatalf("a voice flow that looks the caller up must activate: %v", err)
	}
	if _, err := uc.Execute(context.Background(), "wf-pause"); !errors.Is(err, workflow.ErrNodeIncompatibleScope) {
		t.Fatalf("err = %v, want ErrNodeIncompatibleScope for a pause during a call", err)
	}
}
