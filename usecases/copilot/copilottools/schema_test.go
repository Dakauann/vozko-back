package copilottools

import (
	"reflect"
	"testing"

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
	var f agentFields
	bindArgs(map[string]interface{}{"name": "x", "workspaceId": "evil", "departmentId": "evil"}, &f)
	if in := f.toCreateInput(); in.WorkspaceID != "" {
		t.Fatal("workspace/department must never bind from model args")
	}
	if f.Name == nil || *f.Name != "x" {
		t.Fatal("real fields still bind")
	}
}

func TestAgentSchemasExcludeScope(t *testing.T) {
	create := NewCreateAgentTool(nil).Definition().Parameters
	update := NewUpdateAgentTool(nil, nil).Definition().Parameters
	for _, params := range []map[string]tools.Parameter{create, update} {
		for _, k := range []string{"workspaceId", "workspaceID", "departmentId", "departmentID", "departmentIds"} {
			if _, ok := params[k]; ok {
				t.Fatalf("scope key %q must never appear in a tool schema", k)
			}
		}
	}
}

type taggedArgs struct {
	Query    string   `json:"query" req:"true" desc:"o que procurar"`
	Status   string   `json:"status" enum:"open,closed" desc:"situação"`
	Channels []string `json:"channels" desc:"canais"`
	Page     int      `json:"page"`
}

func TestStructParamsReadsDescriptionsEnumsAndListItemsFromTags(t *testing.T) {
	params, required := structParams(reflect.TypeOf(taggedArgs{}), nil)
	if params["query"].Description != "o que procurar" || len(required) != 1 || required[0] != "query" {
		t.Fatalf("query = %+v required = %v", params["query"], required)
	}
	if !reflect.DeepEqual(params["status"].Enum, []string{"open", "closed"}) {
		t.Fatalf("status enum = %v", params["status"].Enum)
	}
	// Strict providers reject an array parameter that does not say what it holds.
	if params["channels"].Items == nil || params["channels"].Items.Type != "string" {
		t.Fatalf("channels items = %+v", params["channels"].Items)
	}
}

func TestDescriptionMapStillWinsOverTheTag(t *testing.T) {
	params, _ := structParams(reflect.TypeOf(taggedArgs{}), map[string]string{"query": "override"})
	if params["query"].Description != "override" {
		t.Fatalf("description = %q", params["query"].Description)
	}
}

func TestDecodeArgsRefusesMissingRequiredFields(t *testing.T) {
	var got taggedArgs
	if err := decodeArgs(map[string]interface{}{"status": "open"}, &got); err == nil {
		t.Fatal("a missing required field must refuse, not run with an empty value")
	}
	if err := decodeArgs(map[string]interface{}{"query": "   "}, &got); err == nil {
		t.Fatal("a blank required string is missing too")
	}
	if err := decodeArgs(map[string]interface{}{"query": "maria", "page": 2.0, "unknown": "x"}, &got); err != nil || got.Query != "maria" || got.Page != 2 {
		t.Fatalf("decode = %+v, %v", got, err)
	}
}

func TestDecodeArgsRefusesAValueOutsideItsEnum(t *testing.T) {
	var got taggedArgs
	if err := decodeArgs(map[string]interface{}{"query": "x", "status": "deleted"}, &got); err == nil {
		t.Fatal("an enum is a contract, not a suggestion")
	}
}

func TestDefinitionBuildsFromTheArgsStruct(t *testing.T) {
	def := definition("search_x", "procura", taggedArgs{})
	if def.Name != "search_x" || def.Description != "procura" || len(def.Parameters) != 4 || def.Required[0] != "query" {
		t.Fatalf("definition = %+v", def)
	}
}
