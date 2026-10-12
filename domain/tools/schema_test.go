package tools

import (
	"strings"
	"testing"
)

func tableDefinition() Definition {
	return Definition{
		Name:        "studio_edit_image",
		Description: "edita",
		Parameters: map[string]Parameter{
			"operations": {Type: "array", Description: "ops", Items: &ParameterItems{
				Type: "object",
				Properties: map[string]Parameter{
					"op":         {Type: "string", Description: "operação", Enum: []string{"add_table"}},
					"table_rows": {Type: "array", Description: "linhas", Items: &ParameterItems{Type: "array", Items: &ParameterItems{Type: "string"}}},
					"cube":       {Type: "array", Description: "camadas", Items: &ParameterItems{Type: "array", Items: &ParameterItems{Type: "array", Items: &ParameterItems{Type: "integer"}}}},
				},
				Required: []string{"op"},
			}},
			"region": {Type: "object", Description: "região", Items: &ParameterItems{
				Type: "object",
				Properties: map[string]Parameter{
					"x":     {Type: "number", Description: "x"},
					"marks": {Type: "array", Description: "marcas", Items: &ParameterItems{Type: "string"}},
					"box": {Type: "object", Description: "caixa", Items: &ParameterItems{
						Type:       "object",
						Properties: map[string]Parameter{"w": {Type: "number", Description: "largura"}},
						Required:   []string{"w"},
					}},
				},
			}},
		},
		Required: []string{"operations"},
	}
}

func child(t *testing.T, schema map[string]interface{}, path ...string) map[string]interface{} {
	t.Helper()
	node := schema
	for _, key := range path {
		next, ok := node[key].(map[string]interface{})
		if !ok {
			t.Fatalf("%s is missing in %+v", strings.Join(path, "."), node)
		}
		node = next
	}
	return node
}

func TestNestedArraysKeepTheirItemsAtAnyDepth(t *testing.T) {
	schema := tableDefinition().JSONSchema()
	item := child(t, schema, "properties", "operations", "items", "properties")
	rows := child(t, item, "table_rows", "items")
	if rows["type"] != "array" || child(t, rows, "items")["type"] != "string" {
		t.Fatalf("table_rows items = %+v", rows)
	}
	cube := child(t, item, "cube", "items", "items", "items")
	if cube["type"] != "integer" {
		t.Fatalf("cube items = %+v", cube)
	}
}

func TestObjectParametersKeepNestedArraysAndObjects(t *testing.T) {
	region := child(t, tableDefinition().JSONSchema(), "properties", "region")
	if child(t, region, "properties", "marks", "items")["type"] != "string" {
		t.Fatalf("region.marks = %+v", region)
	}
	box := child(t, region, "properties", "box")
	if box["required"].([]string)[0] != "w" || child(t, box, "properties", "w")["type"] != "number" {
		t.Fatalf("region.box = %+v", box)
	}
}

func TestTheRenderedSchemaIsValid(t *testing.T) {
	if issues := SchemaIssues(tableDefinition().JSONSchema()); len(issues) > 0 {
		t.Fatalf("issues: %v", issues)
	}
}

func TestATopLevelFlatToolRendersAsBefore(t *testing.T) {
	def := Definition{Name: "tool", Parameters: map[string]Parameter{
		"  when  ": {Type: "date", Description: "dia"},
		"status":   {Type: "enum", Description: "estado", Enum: []string{"a", "b"}},
	}, Required: []string{"status"}}
	schema := def.JSONSchema()
	when := child(t, schema, "properties", "when")
	if when["type"] != "string" || when["description"] != "dia (formato: YYYY-MM-DD)" || len(when) != 2 {
		t.Fatalf("when = %+v", when)
	}
	status := child(t, schema, "properties", "status")
	if status["type"] != "string" || len(status["enum"].([]string)) != 2 {
		t.Fatalf("status = %+v", status)
	}
	if schema["required"].([]string)[0] != "status" || schema["type"] != "object" {
		t.Fatalf("schema = %+v", schema)
	}
}

func TestAToolWithoutParametersIsAnEmptyObject(t *testing.T) {
	schema := Definition{Name: "tool", Parameters: map[string]Parameter{" ": {Type: "string"}}}.JSONSchema()
	if len(schema) != 1 || schema["type"] != "object" {
		t.Fatalf("schema = %+v", schema)
	}
}

func TestSchemaIssuesCatchWhatProvidersRefuse(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"array without items": {"type": "object", "properties": map[string]interface{}{
			"rows": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "array"}},
		}},
		"unknown type":          {"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "date"}}},
		"missing type":          {"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"description": "x"}}},
		"required not declared": {"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string"}}, "required": []string{"b"}},
		"repeated required":     {"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string"}}, "required": []string{"a", "a"}},
		"empty enum":            {"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string", "enum": []string{}}}},
		"repeated enum":         {"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string", "enum": []string{"x", "x"}}}},
		"enum on a number":      {"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "number", "enum": []string{"1"}}}},
		"unknown keyword":       {"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string", "format": "uuid"}}},
		"root is not an object": {"type": "array", "items": map[string]interface{}{"type": "string"}},
		"items on a string":     {"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string", "items": map[string]interface{}{"type": "string"}}}},
		"blank property name":   {"type": "object", "properties": map[string]interface{}{"": map[string]interface{}{"type": "string"}}},
	}
	for name, schema := range cases {
		if issues := SchemaIssues(schema); len(issues) == 0 {
			t.Errorf("%s passed", name)
		}
	}
}
