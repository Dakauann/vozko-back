package workflow_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/workflow"
)

func TestTheNodeTestSeedsExecuteUntilFromTheChosenTrigger(t *testing.T) {
	c := newConformance(t)
	input := TestNodeInput{NodeID: "copia", Trigger: &TestTrigger{NodeID: "hook", Input: map[string]interface{}{"nome": "Ana"}}}
	out, err := c.execute(conformanceRun{name: "webhook trigger input", wf: multiTriggerWorkflow(), input: input})
	if err != nil || !out.Success {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	if len(out.UpstreamSteps) == 0 || out.UpstreamSteps[0].NodeID != "hook" {
		t.Fatalf("upstream = %+v, want the path to start at the chosen trigger", out.UpstreamSteps)
	}
	if out.StateAfter["resumo"] != "{{input.cpf}}|Ana|{{message}}" {
		t.Fatalf("state = %v, want the webhook body seeded by the production builder", out.StateAfter)
	}
}

func TestTheNodeTestRefusesAnInvalidTriggerInputWithTheSameIssues(t *testing.T) {
	c := newConformance(t)
	input := TestNodeInput{NodeID: "copia", Trigger: &TestTrigger{NodeID: "agente", Input: map[string]interface{}{"cpf": 529.0}}}
	_, err := c.execute(conformanceRun{name: "invalid agent input", wf: multiTriggerWorkflow(), input: input})
	issues, invalid := workflow.TriggerInputIssuesOf(err)
	if !invalid || !reflect.DeepEqual(issues, []workflow.TriggerInputIssue{{Code: workflow.IssueWrongType, Key: "cpf"}}) {
		t.Fatalf("err = %v, issues = %+v", err, issues)
	}

	input.Trigger = &TestTrigger{NodeID: "copia"}
	if _, err := c.execute(conformanceRun{name: "not a trigger", wf: multiTriggerWorkflow(), input: input}); !errors.Is(err, workflow.ErrTriggerInputNotFound) {
		t.Fatalf("err = %v, want trigger not found", err)
	}
}

func TestTheNodeTestOfTheChosenTriggerReturnsTheSeededData(t *testing.T) {
	c := newConformance(t)
	input := TestNodeInput{NodeID: "agente", Trigger: &TestTrigger{NodeID: "agente", Input: map[string]interface{}{"cpf": "529", "agent.name": "Bia"}}}
	out, err := c.execute(conformanceRun{name: "agent trigger step", wf: multiTriggerWorkflow(), input: input})
	if err != nil || !out.Success {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	want := workflow.AgentCallRunData(map[string]interface{}{"cpf": "529"}, "", "Bia")
	if !reflect.DeepEqual(out.ExecutionOutput, want) {
		t.Fatalf("output = %v, want %v", out.ExecutionOutput, want)
	}
}

func TestAChosenTriggerThatDoesNotReachTheTargetStopsWithNoPath(t *testing.T) {
	c := newConformance(t)
	wf := multiTriggerWorkflow()
	wf.Graph.Edges = append(wf.Graph.Edges[1:], workflow.Edge{Source: "agente", Target: "end"})
	input := TestNodeInput{NodeID: "copia", Trigger: &TestTrigger{NodeID: "agente", Input: map[string]interface{}{"cpf": "529"}}}
	out, err := c.execute(conformanceRun{name: "unreachable from trigger", wf: wf, input: input})
	if err != nil || out.Success || out.PathStop == nil || out.PathStop.Reason != PathStopNoPath {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
}

func TestTheNodeAnalysisNamesTheTriggerItsPathStartsFrom(t *testing.T) {
	repo := NewMockWorkflowRepository()
	wf := multiTriggerWorkflow()
	wf.Graph.Nodes = append(wf.Graph.Nodes, workflow.Node{ID: "so_msg", Type: workflow.NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "x", "value": "{{message}}"}})
	wf.Graph.Edges = append(wf.Graph.Edges, workflow.Edge{Source: "msg", Target: "so_msg"})
	_ = repo.Create(wf)
	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo})

	out, err := uc.Analyze(context.Background(), AnalyzeNodeInput{WorkflowID: "wf-1", WorkspaceID: "ws1", NodeID: "copia"})
	if err != nil {
		t.Fatal(err)
	}
	ui := out.ToUIAnalysis()
	if ui.Trigger == nil || ui.Trigger.NodeID != "agente" || ui.Trigger.Shape != workflow.TriggerInputFields || ui.Trigger.Fields[0].Key != "cpf" {
		t.Fatalf("trigger = %+v, want the agent-call spec the path starts from", ui.Trigger)
	}
	encoded, _ := json.Marshal(ui)
	if !strings.Contains(string(encoded), `"trigger":{"nodeId":"agente"`) {
		t.Fatalf("analysis = %s", encoded)
	}

	out, err = uc.Analyze(context.Background(), AnalyzeNodeInput{WorkflowID: "wf-1", WorkspaceID: "ws1", NodeID: "so_msg"})
	if err != nil {
		t.Fatal(err)
	}
	if ui := out.ToUIAnalysis(); ui.Trigger != nil {
		t.Fatalf("a path from a message trigger has no trigger input, got %+v", ui.Trigger)
	}
	if encoded, _ := json.Marshal(out.ToUIAnalysis()); strings.Contains(string(encoded), `"trigger"`) {
		t.Fatalf("the field must be omitted: %s", encoded)
	}
}
