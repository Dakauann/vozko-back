package tools

import "testing"

func TestResolveParamType(t *testing.T) {
	cases := []struct {
		raw        string
		wantSchema string
		wantFormat bool // whether a non-empty format hint is expected
	}{
		// Semantic aliases collapse to a JSON-Schema primitive + format hint.
		{"date", "string", true},
		{"time", "string", true},
		{"datetime", "string", true},
		{"email", "string", true},
		{"phone", "string", true},
		{"enum", "string", false}, // enum maps to string but carries no format hint
		// Case-insensitive: a stored "Email" must resolve like "email" so it can't
		// leak through to the LLM as an invalid type.
		{"Email", "string", true},
		{"  DATE ", "string", true},
		// Base JSON-Schema primitives pass through unchanged.
		{"string", "string", false},
		{"integer", "integer", false},
		{"object", "object", false},
		// Unknown types pass through (the builder validation is what rejects them).
		{"text", "text", false},
	}
	for _, c := range cases {
		gotSchema, gotFormat := ResolveParamType(c.raw)
		if gotSchema != c.wantSchema {
			t.Errorf("ResolveParamType(%q) schema = %q, want %q", c.raw, gotSchema, c.wantSchema)
		}
		if (gotFormat != "") != c.wantFormat {
			t.Errorf("ResolveParamType(%q) format = %q, wantNonEmpty=%v", c.raw, gotFormat, c.wantFormat)
		}
	}
}

func TestIsValidParamType(t *testing.T) {
	valid := []string{
		"string", "number", "integer", "boolean", "array", "object",
		"date", "time", "datetime", "email", "phone", "enum",
		"INTEGER", "Email", "  date  ", "", // case/space-insensitive; empty defaults to string
	}
	for _, v := range valid {
		if !IsValidParamType(v) {
			t.Errorf("IsValidParamType(%q) = false, want true", v)
		}
	}
	invalid := []string{"text", "int", "float", "url", "str", "bool"}
	for _, v := range invalid {
		if IsValidParamType(v) {
			t.Errorf("IsValidParamType(%q) = true, want false", v)
		}
	}
}

// TestParamTypeListsAgree guards the single source: every alias and base type must
// pass IsValidParamType and appear in AllowedParamTypes, so the human-facing list,
// the validation, and the runtime mapping can't silently diverge.
func TestParamTypeListsAgree(t *testing.T) {
	listed := make(map[string]bool)
	for _, t := range AllowedParamTypes() {
		listed[t] = true
	}
	for alias := range paramTypeAliases {
		if !listed[alias] {
			t.Errorf("alias %q missing from AllowedParamTypes()", alias)
		}
		if !IsValidParamType(alias) {
			t.Errorf("alias %q not accepted by IsValidParamType", alias)
		}
	}
	for base := range baseJSONSchemaTypes {
		if !listed[base] {
			t.Errorf("base type %q missing from AllowedParamTypes()", base)
		}
		if !IsValidParamType(base) {
			t.Errorf("base type %q not accepted by IsValidParamType", base)
		}
	}
}
