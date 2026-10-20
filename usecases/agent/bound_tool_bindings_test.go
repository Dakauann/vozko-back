package agent_usecase

import (
	"testing"

	"vozko/domain/agent"
	toolsdomain "vozko/domain/tools"
)

func indexWithBoundWorkflowTool() *toolRegistryIndex {
	idx := indexWith("http_request")
	idx.defs["run_workflow"] = toolsdomain.Definition{
		Name:           "run_workflow",
		BindingKey:     "workflow_id",
		ConfigSchema:   map[string]toolsdomain.ConfigParameter{"workflow_id": {Type: "string", Required: true}},
		RequiredConfig: []string{"workflow_id"},
	}
	idx.all = append(idx.all, "run_workflow")
	return idx
}

func workflowBinding(id string) agent.ToolBinding {
	return agent.ToolBinding{Name: "run_workflow", Config: map[string]interface{}{"workflow_id": id}}
}

func TestAnAgentKeepsOneBindingPerBoundWorkflow(t *testing.T) {
	idx := indexWithBoundWorkflowTool()
	requested := []agent.ToolBinding{workflowBinding("wf-a"), workflowBinding("wf-b"), workflowBinding("wf-a"), {Name: "http_request"}, {Name: "http_request"}}

	saved, err := validateRequestedToolBindings(idx, requested)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 3 || saved[1].Config["workflow_id"] != "wf-a" || saved[2].Config["workflow_id"] != "wf-b" {
		t.Fatalf("saved = %+v, want http_request and one binding per workflow, in request order", saved)
	}

	kept := filterExistingToolBindings(idx, requested)
	if len(kept) != 3 {
		t.Fatalf("kept = %+v", kept)
	}
}
