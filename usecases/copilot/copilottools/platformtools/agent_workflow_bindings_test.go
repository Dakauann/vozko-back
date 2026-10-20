package platformtools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/domain/tools"
)

func workflowCatalog() toolDefinitions {
	return toolDefinitions{
		{Name: "run_workflow", DisplayName: "Executar fluxo", BindingKey: "workflow_id"},
		{Name: "manage_lead_memory", DisplayName: "Memórias"},
	}
}

func agentWithWorkflows(workflowIDs ...string) *agent.Agent {
	a := boundAgent()
	for _, id := range workflowIDs {
		a.InternalTools = append(a.InternalTools, agent.ToolBinding{Name: "run_workflow", Config: map[string]interface{}{"workflow_id": id, "note": "old"}})
	}
	return a
}

func workflowBindings(in agent.UpdateAgentInput) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	for _, tb := range in.InternalTools {
		if tb.Name == "run_workflow" {
			id, _ := tb.Config["workflow_id"].(string)
			out[id] = tb.Config
		}
	}
	return out
}

func updateWith(t *testing.T, current *agent.Agent, args map[string]interface{}) agent.UpdateAgentInput {
	t.Helper()
	upd := &fakeUpdateAgent{}
	args["id"] = current.ID
	res := NewUpdateAgentTool(fakeGetAgent{a: current}, upd, AgentDeps{Catalog: workflowCatalog()}).Execute(context.Background(), okContext(), args)
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %v (%s)", res.Status, res.Message)
	}
	return upd.got
}

func runWorkflowArg(workflowID string) map[string]interface{} {
	return map[string]interface{}{"name": "run_workflow", "config": map[string]interface{}{"workflow_id": workflowID}}
}

func TestEloAddsASecondWorkflowWithoutDroppingTheFirst(t *testing.T) {
	got := workflowBindings(updateWith(t, agentWithWorkflows("wf-1"), map[string]interface{}{
		"addTools": []interface{}{runWorkflowArg("wf-2")},
	}))
	if len(got) != 2 || got["wf-1"] == nil || got["wf-2"] == nil {
		t.Fatalf("workflow bindings = %+v, want both workflows", got)
	}
}

func TestEloReAddingAWorkflowReplacesOnlyThatBinding(t *testing.T) {
	got := workflowBindings(updateWith(t, agentWithWorkflows("wf-1", "wf-2"), map[string]interface{}{
		"addTools": []interface{}{runWorkflowArg("wf-2")},
	}))
	if len(got) != 2 || got["wf-1"]["note"] != "old" || got["wf-2"]["note"] != nil {
		t.Fatalf("workflow bindings = %+v, want wf-2 replaced and wf-1 kept", got)
	}
}

func TestEloRemovesOneWorkflowByItsToolName(t *testing.T) {
	got := workflowBindings(updateWith(t, agentWithWorkflows("wf-1", "wf-2"), map[string]interface{}{
		"removeTools": []interface{}{tools.BoundToolName("run_workflow", "wf-1")},
	}))
	if len(got) != 1 || got["wf-2"] == nil {
		t.Fatalf("workflow bindings = %+v, want only wf-2 left", got)
	}
}

func TestEloRemovingTheBaseNameRemovesEveryWorkflow(t *testing.T) {
	in := updateWith(t, agentWithWorkflows("wf-1", "wf-2"), map[string]interface{}{
		"removeTools": []interface{}{"run_workflow"},
	})
	if got := workflowBindings(in); len(got) != 0 {
		t.Fatalf("workflow bindings = %+v", got)
	}
	if len(in.InternalTools) != 2 {
		t.Fatalf("tools = %+v, the other tools must stay", in.InternalTools)
	}
}

func TestTheToolDescriptionsTeachOneBindingPerWorkflow(t *testing.T) {
	params := NewUpdateAgentTool(nil, nil, AgentDeps{}).Definition().Parameters
	if !strings.Contains(params["addTools"].Description, "run_workflow") || !strings.Contains(params["removeTools"].Description, "run_workflow_<id do fluxo>") {
		t.Fatalf("addTools = %q\nremoveTools = %q", params["addTools"].Description, params["removeTools"].Description)
	}
}
