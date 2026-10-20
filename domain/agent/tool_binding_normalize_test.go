package agent

import "testing"

func TestNormalizingAnAgentKeepsBindingsThatDifferOnlyInConfig(t *testing.T) {
	binding := func(id string) ToolBinding {
		return ToolBinding{Name: "Run_Workflow", Config: map[string]interface{}{"workflow_id": id}}
	}
	a := &Agent{InternalTools: []ToolBinding{binding("wf-a"), binding("wf-b"), binding("wf-a"), {Name: "http_request"}, {Name: "HTTP_REQUEST"}}}

	a.Normalize()

	if len(a.InternalTools) != 3 {
		t.Fatalf("bindings = %+v, want exact duplicates dropped and both workflows kept", a.InternalTools)
	}
	if a.InternalTools[0].Name != "run_workflow" || a.InternalTools[1].Config["workflow_id"] != "wf-b" || a.InternalTools[2].Name != "http_request" {
		t.Fatalf("bindings = %+v", a.InternalTools)
	}
}
