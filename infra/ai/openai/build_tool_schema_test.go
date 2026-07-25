package openai

import (
	"reflect"
	"testing"

	tools "vozko/domain/tools"
)

func TestBuildToolSchema_WithEnum(t *testing.T) {
	def := tools.Definition{
		Name:        "check_availability",
		Description: "Check calendar availability",
		Parameters: map[string]tools.Parameter{
			"format": {
				Type:        "string",
				Description: "Date format type",
				Enum:        []string{"date_time", "time", "date"},
			},
			"notes": {
				Type:        "string",
				Description: "Additional notes",
			},
		},
		Required: []string{"format"},
	}

	schema := buildToolSchema(def)

	props := schema["properties"].(map[string]interface{})

	formatProp := props["format"].(map[string]interface{})
	if formatProp["type"] != "string" {
		t.Fatalf("expected type 'string', got %v", formatProp["type"])
	}
	enumVal, ok := formatProp["enum"]
	if !ok {
		t.Fatal("expected 'enum' key in format property")
	}
	if !reflect.DeepEqual(enumVal, []string{"date_time", "time", "date"}) {
		t.Fatalf("expected enum [date_time, time, date], got %v", enumVal)
	}

	notesProp := props["notes"].(map[string]interface{})
	if _, hasEnum := notesProp["enum"]; hasEnum {
		t.Fatal("notes property should not have enum key")
	}

	required := schema["required"].([]string)
	if !reflect.DeepEqual(required, []string{"format"}) {
		t.Fatalf("expected required=[format], got %v", required)
	}
}

func TestBuildToolSchema_EmptyEnum(t *testing.T) {
	def := tools.Definition{
		Parameters: map[string]tools.Parameter{
			"field": {
				Type:        "string",
				Description: "A field",
				Enum:        []string{},
			},
		},
	}

	schema := buildToolSchema(def)
	props := schema["properties"].(map[string]interface{})
	fieldProp := props["field"].(map[string]interface{})

	if _, hasEnum := fieldProp["enum"]; hasEnum {
		t.Fatal("empty enum slice should not produce 'enum' key")
	}
}

func TestBuildToolSchema_NilEnum(t *testing.T) {
	def := tools.Definition{
		Parameters: map[string]tools.Parameter{
			"field": {
				Type:        "string",
				Description: "A field",
				Enum:        nil,
			},
		},
	}

	schema := buildToolSchema(def)
	props := schema["properties"].(map[string]interface{})
	fieldProp := props["field"].(map[string]interface{})

	if _, hasEnum := fieldProp["enum"]; hasEnum {
		t.Fatal("nil enum should not produce 'enum' key")
	}
}

func TestBuildToolSchema_WhitespaceKeysTrimmed(t *testing.T) {
	def := tools.Definition{
		Parameters: map[string]tools.Parameter{
			"  padded  ": {
				Type:        "string",
				Description: "A padded field",
			},
		},
	}

	schema := buildToolSchema(def)
	props := schema["properties"].(map[string]interface{})

	if _, ok := props["padded"]; !ok {
		t.Fatalf("expected trimmed key 'padded' in properties, got keys: %v", props)
	}
}

func TestBuildToolSchema_RichTypes(t *testing.T) {
	tests := []struct {
		paramType   string
		wantType    string
		wantHint    string
		description string
	}{
		{"date", "string", "(formato: YYYY-MM-DD)", "user desc"},
		{"time", "string", "(formato: HH:MM)", "user desc"},
		{"datetime", "string", "(formato: YYYY-MM-DDTHH:MM:SS-03:00, ISO 8601)", "user desc"},
		{"email", "string", "(formato: email válido)", "user desc"},
		{"phone", "string", "(formato: número com DDI, ex: +5511999999999)", "user desc"},
		{"enum", "string", "", "user desc"},
		{"string", "string", "", "user desc"},
		{"number", "number", "", "user desc"},
		{"boolean", "boolean", "", "user desc"},
	}

	for _, tt := range tests {
		t.Run(tt.paramType, func(t *testing.T) {
			def := tools.Definition{
				Parameters: map[string]tools.Parameter{
					"field": {
						Type:        tt.paramType,
						Description: tt.description,
					},
				},
			}

			schema := buildToolSchema(def)
			props := schema["properties"].(map[string]interface{})
			prop := props["field"].(map[string]interface{})

			if prop["type"] != tt.wantType {
				t.Errorf("type=%q: got schema type %q, want %q", tt.paramType, prop["type"], tt.wantType)
			}

			desc := prop["description"].(string)
			if tt.wantHint != "" {
				if !contains(desc, tt.wantHint) {
					t.Errorf("type=%q: description %q should contain hint %q", tt.paramType, desc, tt.wantHint)
				}
				if !contains(desc, tt.description) {
					t.Errorf("type=%q: description %q should still contain original %q", tt.paramType, desc, tt.description)
				}
			} else {
				if desc != tt.description {
					t.Errorf("type=%q: description should be unchanged %q, got %q", tt.paramType, tt.description, desc)
				}
			}
		})
	}
}

func TestBuildToolSchema_RichTypeEmptyDescription(t *testing.T) {
	def := tools.Definition{
		Parameters: map[string]tools.Parameter{
			"field": {
				Type:        "date",
				Description: "",
			},
		},
	}

	schema := buildToolSchema(def)
	props := schema["properties"].(map[string]interface{})
	prop := props["field"].(map[string]interface{})

	desc := prop["description"].(string)
	if desc != "(formato: YYYY-MM-DD)" {
		t.Errorf("expected just the format hint, got %q", desc)
	}
}

func TestBuildToolSchema_EnumTypeWithValues(t *testing.T) {
	def := tools.Definition{
		Parameters: map[string]tools.Parameter{
			"period": {
				Type:        "enum",
				Description: "Período do dia",
				Enum:        []string{"morning", "afternoon", "evening"},
			},
		},
	}

	schema := buildToolSchema(def)
	props := schema["properties"].(map[string]interface{})
	prop := props["period"].(map[string]interface{})

	if prop["type"] != "string" {
		t.Errorf("enum type should map to 'string', got %q", prop["type"])
	}
	enumVal := prop["enum"].([]string)
	if len(enumVal) != 3 || enumVal[0] != "morning" {
		t.Errorf("expected enum values, got %v", enumVal)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
