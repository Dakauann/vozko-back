package copilottools

import (
	"reflect"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/tools"
)

type schemaFake struct {
	Name   string   `json:"name" req:"true"`
	Opt    string   `json:"opt,omitempty"`
	Flag   *bool    `json:"flag"`
	Count  int      `json:"count"`
	List   []string `json:"list"`
	Hidden string   `json:"-"`
	NoTag  string
}

func TestStructParams(t *testing.T) {
	desc := map[string]string{"name": "the name", "flag": "a flag"}
	params, required := structParams(reflect.TypeOf(schemaFake{}), desc)

	for _, k := range []string{"name", "opt", "flag", "count", "list"} {
		if _, ok := params[k]; !ok {
			t.Fatalf("missing param %q", k)
		}
	}
	if _, ok := params["Hidden"]; ok {
		t.Fatal(`json:"-" field must be skipped`)
	}
	if _, ok := params["NoTag"]; ok {
		t.Fatal("untagged field must be skipped")
	}
	if params["name"].Type != "string" || params["flag"].Type != "boolean" ||
		params["count"].Type != "integer" || params["list"].Type != "array" {
		t.Fatalf("param types wrong: %+v", params)
	}
	if params["name"].Description != "the name" || params["opt"].Description != "" {
		t.Fatalf("descriptions wrong: %+v", params)
	}
	if len(required) != 1 || required[0] != "name" {
		t.Fatalf("required wrong: %v", required)
	}
}

func TestBindArgs(t *testing.T) {
	var f schemaFake
	bindArgs(map[string]interface{}{"name": "x", "flag": true, "count": 3}, &f)
	if f.Name != "x" || f.Flag == nil || !*f.Flag || f.Count != 3 {
		t.Fatalf("bind wrong: %+v", f)
	}

	var g schemaFake
	bindArgs(map[string]interface{}{"name": make(chan int)}, &g)
	if g.Name != "" {
		t.Fatal("unmarshalable args must be a no-op")
	}
}

func TestBindArgsOnlyBindsSchemaFields(t *testing.T) {
	var f schemaFake
	bindArgs(map[string]interface{}{
		"name": "x", "NoTag": "leak", "Hidden": "leak",
		"workspaceId": "leak", "createdBy": "leak", "id": "leak",
	}, &f)
	if f.Name != "x" {
		t.Fatal("schema fields must bind")
	}
	if f.NoTag != "" || f.Hidden != "" {
		t.Fatalf("only fields exposed by the schema may bind, got %+v", f)
	}
}

func TestCreateInputRejectsScopeFromArgs(t *testing.T) {
	var in agent.CreateAgentInput
	bindArgs(map[string]interface{}{"name": "x", "workspaceId": "evil", "departmentId": "evil"}, &in)
	if in.WorkspaceID != "" {
		t.Fatal("workspace/department must never bind from model args")
	}
	if in.Name != "x" {
		t.Fatal("real fields still bind")
	}
}

func TestAgentSchemasExcludeScope(t *testing.T) {
	create, _ := structParams(reflect.TypeOf(agent.CreateAgentInput{}), agentToolDescriptions)
	update, _ := structParams(reflect.TypeOf(agent.UpdateAgentInput{}), agentToolDescriptions)
	for _, params := range []map[string]tools.Parameter{create, update} {
		for _, k := range []string{"workspaceId", "workspaceID", "departmentId", "departmentID", "departmentIds"} {
			if _, ok := params[k]; ok {
				t.Fatalf("scope key %q must never appear in a tool schema", k)
			}
		}
	}
}
