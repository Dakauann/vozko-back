package platformtools

import (
	"testing"

	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
)

func TestCreateInputRejectsScopeFromArgs(t *testing.T) {
	var f agentFields
	copilottools.BindArgs(map[string]interface{}{"name": "x", "workspaceId": "evil", "departmentId": "evil"}, &f)
	if in := f.toCreateInput(); in.WorkspaceID != "" {
		t.Fatal("workspace/department must never bind from model args")
	}
	if f.Name == nil || *f.Name != "x" {
		t.Fatal("real fields still bind")
	}
}

func TestAgentSchemasExcludeScope(t *testing.T) {
	create := NewCreateAgentTool(nil, AgentDeps{}).Definition().Parameters
	update := NewUpdateAgentTool(nil, nil, AgentDeps{}).Definition().Parameters
	for _, params := range []map[string]tools.Parameter{create, update} {
		for _, k := range []string{"workspaceId", "workspaceID", "departmentId", "departmentID", "departmentIds"} {
			if _, ok := params[k]; ok {
				t.Fatalf("scope key %q must never appear in a tool schema", k)
			}
		}
	}
}
