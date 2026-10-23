package workflow_usecase

import (
	"context"
	"reflect"
	"testing"

	"vozko/domain/workflow"
)

func TestAStepRecordsTheReferencesItSentEmptyToTheCustomer(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionSendText, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		text := workflow.CustomerText(ctx, ctx.Node.Config["text"].(string))
		return &workflow.NodeResult{Output: map[string]interface{}{"text": text}}, nil
	})
	h.startWith(linearWorkflow(workflow.Node{ID: "ola", Type: workflow.NodeTypeActionSendText, Config: map[string]interface{}{
		"text": "Oi {{lead_name}}, pago: {{pago}} {{input.telefone}}",
	}}), func(run *workflow.WorkflowRun) {
		run.State.Set("lead_name", "Ana")
		run.State.Set("pago", true)
	})

	step := h.step(t, "ola")
	if !reflect.DeepEqual(step.UnresolvedRefs, []string{"input.telefone"}) {
		t.Fatalf("unresolvedRefs = %v", step.UnresolvedRefs)
	}
	if step.Input["text"] != "Oi Ana, pago: Sim " || step.Output["text"] != "Oi Ana, pago: Sim " {
		t.Fatalf("params = %v output = %v, want both to show what the customer got", step.Input["text"], step.Output["text"])
	}
}

func TestAStepThatSendsNothingRecordsNoUnresolvedReferences(t *testing.T) {
	h := newTraceHarness()
	h.register(workflow.NodeTypeActionHTTPRequest, func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{Output: map[string]interface{}{}}, nil
	})
	h.start(linearWorkflow(workflow.Node{ID: "n55", Type: workflow.NodeTypeActionHTTPRequest, Config: map[string]interface{}{"url": "https://api/{{input.cpf}}"}}))

	if refs := h.step(t, "n55").UnresolvedRefs; refs != nil {
		t.Fatalf("unresolvedRefs = %v", refs)
	}
}

func TestTheUnresolvedReferencesGoThroughTheStepRedactor(t *testing.T) {
	entry := &workflow.WorkflowRunLog{UnresolvedRefs: []string{"input.cpf", "52998224725"}}
	if got := redactedStep(entry).UnresolvedRefs; got[0] != "input.cpf" || got[1] == "52998224725" {
		t.Fatalf("unresolvedRefs = %v", got)
	}
}

func TestTheNodeTestShowsTheResolvedParamsAndTheRefsSentEmpty(t *testing.T) {
	repo := NewMockWorkflowRepository()
	registry := NewNodeExecutorRegistry()
	registry.Register(workflow.NodeTypeActionSendText, traceExecutor{nodeType: workflow.NodeTypeActionSendText, run: func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
		return &workflow.NodeResult{Output: map[string]interface{}{"text": workflow.CustomerText(ctx, ctx.Node.Config["text"].(string))}}, nil
	}})
	nodes := []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerMessageReceived},
		{ID: "ola", Type: workflow.NodeTypeActionSendText, Config: map[string]interface{}{
			"text":    "Oi {{lead_name}} {{input.telefone}}",
			"buttons": []interface{}{"{{lead_name}}"},
		}},
	}
	_ = repo.Create(createTestWorkflow("wf1", "ws1", nodes, []workflow.Edge{{Source: "trigger1", Target: "ola"}}))
	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Steps: registry, Executors: fixedExecutors(registry)})

	result, err := uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "ola", TriggerVars: map[string]interface{}{"lead_name": "Ana"}})

	if err != nil || !result.Success {
		t.Fatalf("result = %+v err = %v", result, err)
	}
	if result.InterpolatedConfig["text"] != "Oi Ana " || !reflect.DeepEqual(result.InterpolatedConfig["buttons"], []interface{}{"Ana"}) {
		t.Fatalf("interpolated_config = %v", result.InterpolatedConfig)
	}
	if result.Step.Input["text"] != "Oi Ana " || !reflect.DeepEqual(result.Step.UnresolvedRefs, []string{"input.telefone"}) {
		t.Fatalf("step = %+v", result.Step)
	}
}
