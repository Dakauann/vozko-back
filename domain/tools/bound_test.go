package tools

import (
	"strings"
	"testing"
)

func TestASchemaObjectParameterListsTheDeclaredFields(t *testing.T) {
	raw := []interface{}{
		map[string]interface{}{"name": "cpf", "type": "string", "description": "CPF do candidato", "required": true, "example": "12345678900"},
		map[string]interface{}{"name": "idade", "type": "number", "description": "Idade"},
	}

	param, section, ok := SchemaObjectParameter("Campos do fluxo", "Campos", raw)

	if !ok || param.Type != "object" {
		t.Fatalf("param = %+v, ok = %v", param, ok)
	}
	if param.Description != "Campos do fluxo. Fields: cpf: string (required), idade: number" {
		t.Fatalf("description = %q", param.Description)
	}
	for _, fragment := range []string{"Campos:", "- cpf (string) [REQUIRED]: CPF do candidato (e.g., 12345678900)", "- idade (number): Idade"} {
		if !strings.Contains(section, fragment) {
			t.Fatalf("section misses %q:\n%s", fragment, section)
		}
	}
}

func TestASchemaWithoutNamedFieldsGivesNoParameter(t *testing.T) {
	if _, _, ok := SchemaObjectParameter("x", "y", []interface{}{map[string]interface{}{"type": "string"}}); ok {
		t.Fatal("a schema with no named field must give no parameter")
	}
	if _, _, ok := SchemaObjectParameter("x", "y", nil); ok {
		t.Fatal("an empty schema must give no parameter")
	}
}

func TestHTTPConfigExpansionKeepsItsBodyParameter(t *testing.T) {
	def := Definition{Name: "http_request", Description: "base"}.WithConfigExpansion(map[string]interface{}{
		"body_schema": []interface{}{map[string]interface{}{"name": "cpf", "type": "string", "required": true}},
	})
	if def.Parameters["body_values"].Description != "JSON body fields to send. Fields: cpf: string (required)" {
		t.Fatalf("body_values = %+v", def.Parameters["body_values"])
	}
	if !strings.Contains(def.Description, "Body Parameters:\n  - cpf (string) [REQUIRED]: ") {
		t.Fatalf("description = %q", def.Description)
	}
}

func TestBoundToolNamesAreStableAndSafeForEveryProvider(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"669a1bad-1111-4222-8333-444455556666", "run_workflow_669a1bad-1111-4222-8333-444455556666"},
		{"  WF.Matrícula 2 ", "run_workflow_wf_matr_cula_2"},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range cases {
		got := BoundToolName("run_workflow", tc.key)
		if got != tc.want {
			t.Fatalf("BoundToolName(%q) = %q, want %q", tc.key, got, tc.want)
		}
		if got != "" && !ValidToolName(got) {
			t.Fatalf("%q breaks the provider tool-name rule", got)
		}
	}
	long := BoundToolName("run_workflow", strings.Repeat("a", 100))
	if len(long) != MaxToolNameLength || !ValidToolName(long) {
		t.Fatalf("long name = %q (%d)", long, len(long))
	}
}

func TestABindingKeyMakesEachBindingItsOwnTool(t *testing.T) {
	bound := Definition{Name: "run_workflow", BindingKey: "workflow_id"}
	a := map[string]interface{}{"workflow_id": "wf-a"}
	b := map[string]interface{}{"workflow_id": "wf-b"}
	if bound.ToolNameFor(a) != "run_workflow_wf-a" || bound.BindingIdentity(a) == bound.BindingIdentity(b) {
		t.Fatalf("names = %q / %q", bound.ToolNameFor(a), bound.ToolNameFor(b))
	}
	if bound.ToolNameFor(map[string]interface{}{}) != "" {
		t.Fatal("a binding without its key has no tool name")
	}

	single := Definition{Name: "http_request"}
	if single.ToolNameFor(a) != "http_request" || single.BindingIdentity(a) != single.BindingIdentity(b) {
		t.Fatal("a tool without a binding key stays one tool whatever its config")
	}
}

func TestABoundToolNameBelongsToItsBase(t *testing.T) {
	if !IsBoundToolName("run_workflow_abc", "run_workflow") || !IsBoundToolName("RUN_WORKFLOW_abc", "run_workflow") {
		t.Fatal("a bound name must route to its base")
	}
	for _, name := range []string{"run_workflow", "run_workflow_", "run_workflows", "http_request_abc"} {
		if IsBoundToolName(name, "run_workflow") {
			t.Fatalf("%q must not route to run_workflow", name)
		}
	}
}

func TestTheBindingKeyIsReadBackFromABoundName(t *testing.T) {
	name := BoundToolName("run_workflow", "5f0c2a8e-1b7d-4c1e-9a3f-2d6b8e4f7a10")
	if key, ok := BoundKeyOf(name, "run_workflow"); !ok || key != "5f0c2a8e-1b7d-4c1e-9a3f-2d6b8e4f7a10" {
		t.Fatalf("key = %q, ok = %v", key, ok)
	}
	for _, name := range []string{"run_workflow", "run_workflow_", "http_request"} {
		if _, ok := BoundKeyOf(name, "run_workflow"); ok {
			t.Fatalf("%q has no binding key of run_workflow", name)
		}
	}
}
