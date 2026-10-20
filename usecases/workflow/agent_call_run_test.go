package workflow_usecase

import (
	"testing"

	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

func TestAnAgentStartedRunReadsItsInputsAndLeavesItsReplyOnTheRun(t *testing.T) {
	h := newTraceHarness()
	h.registry.Register(workflow.NodeTypeActionReplyToAgent, node_executors.NewReplyToAgentExecutor())
	w := &workflow.Workflow{ID: "wf-1", WorkspaceID: "ws-1", Type: workflow.WorkflowTypeMessages, Graph: workflow.Graph{
		Nodes: []workflow.Node{
			{ID: "t", Type: workflow.NodeTypeTriggerAgentCall},
			{ID: "r", Type: workflow.NodeTypeActionReplyToAgent, Config: map[string]interface{}{workflow.ReplyToAgentFieldsKey: []interface{}{
				map[string]interface{}{"key": "curso", "value": "{{input.curso}} para {{agent.name}}"},
			}}},
			{ID: "end", Type: workflow.NodeTypeEnd},
		},
		Edges: []workflow.Edge{{Source: "t", Target: "r"}, {Source: "r", Target: "end"}},
	}}

	run := h.startWith(w, func(run *workflow.WorkflowRun) {
		run.TriggerType = workflow.TriggerAgentCall
		for k, v := range workflow.AgentCallRunData(map[string]interface{}{"curso": "ADM"}, "agent-1", "Bia") {
			run.State.Set(k, v)
		}
	})

	stored, _ := h.runs.FindByID(run.ID)
	if stored.Status != workflow.RunStatusCompleted || stored.AgentResult["curso"] != "ADM para Bia" {
		t.Fatalf("stored run = %+v", stored)
	}
	if got := h.step(t, "r").Output["curso"]; got != "ADM para Bia" {
		t.Fatalf("step output = %v", got)
	}
}
