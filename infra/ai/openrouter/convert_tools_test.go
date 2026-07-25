package openrouter

import (
	"testing"

	"vozko/domain/tools"
)

func TestConvertTools_ResolveParamType(t *testing.T) {
	tests := []struct {
		name        string
		paramType   string
		wantType    string
		wantDescSub string
	}{
		{"date", "date", "string", "(formato: YYYY-MM-DD)"},
		{"time", "time", "string", "(formato: HH:MM)"},
		{"datetime", "datetime", "string", "(formato: YYYY-MM-DDTHH:MM:SS-03:00, ISO 8601)"},
		{"email", "email", "string", "(formato: email válido)"},
		{"phone", "phone", "string", "(formato: número com DDI, ex: +5511999999999)"},
		{"enum", "enum", "string", ""},
		{"string_passthrough", "string", "string", ""},
		{"number_passthrough", "number", "number", ""},
		{"boolean_passthrough", "boolean", "boolean", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := tools.Definition{
				Name:        "test_tool",
				Description: "test",
				Parameters: map[string]tools.Parameter{
					"param1": {Type: tt.paramType, Description: "base desc"},
				},
			}
			result := convertTools([]tools.Definition{def})
			if len(result) != 1 {
				t.Fatalf("expected 1 tool, got %d", len(result))
			}
			schema := result[0].Function.Parameters.(map[string]interface{})
			props := schema["properties"].(map[string]interface{})
			prop := props["param1"].(map[string]interface{})

			if got := prop["type"].(string); got != tt.wantType {
				t.Errorf("type = %q, want %q", got, tt.wantType)
			}
			desc := prop["description"].(string)
			if tt.wantDescSub != "" {
				if !contains(desc, tt.wantDescSub) {
					t.Errorf("description = %q, want substring %q", desc, tt.wantDescSub)
				}
			}
		})
	}
}

func TestConvertTools_DatetimeEmptyDescription(t *testing.T) {
	def := tools.Definition{
		Name: "tool",
		Parameters: map[string]tools.Parameter{
			"start": {Type: "datetime", Description: ""},
		},
	}
	result := convertTools([]tools.Definition{def})
	schema := result[0].Function.Parameters.(map[string]interface{})
	props := schema["properties"].(map[string]interface{})
	prop := props["start"].(map[string]interface{})

	if got := prop["type"].(string); got != "string" {
		t.Errorf("type = %q, want string", got)
	}
	desc := prop["description"].(string)
	if desc != "(formato: YYYY-MM-DDTHH:MM:SS-03:00, ISO 8601)" {
		t.Errorf("description = %q, want format hint only", desc)
	}
}

func TestConvertTools_EnumWithValues(t *testing.T) {
	def := tools.Definition{
		Name: "tool",
		Parameters: map[string]tools.Parameter{
			"status": {
				Type:        "enum",
				Description: "Status da reunião",
				Enum:        []string{"confirmed", "pending", "cancelled"},
			},
		},
	}
	result := convertTools([]tools.Definition{def})
	schema := result[0].Function.Parameters.(map[string]interface{})
	props := schema["properties"].(map[string]interface{})
	prop := props["status"].(map[string]interface{})

	if got := prop["type"].(string); got != "string" {
		t.Errorf("type = %q, want string", got)
	}
	enumVals, ok := prop["enum"].([]string)
	if !ok {
		t.Fatalf("enum not present or wrong type")
	}
	if len(enumVals) != 3 {
		t.Errorf("enum length = %d, want 3", len(enumVals))
	}
}

func TestConvertTools_WhitespaceKeysTrimmed(t *testing.T) {
	def := tools.Definition{
		Name: "tool",
		Parameters: map[string]tools.Parameter{
			"  name  ": {Type: "string", Description: "Nome"},
		},
	}
	result := convertTools([]tools.Definition{def})
	schema := result[0].Function.Parameters.(map[string]interface{})
	props := schema["properties"].(map[string]interface{})
	if _, ok := props["name"]; !ok {
		t.Error("expected trimmed key 'name' in properties")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (substr == "" || searchSubstring(s, substr))
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
