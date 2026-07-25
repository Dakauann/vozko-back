package agent

import (
	"testing"

	agent "vozko/domain/agent"
)

func TestEncodeAgentVariables(t *testing.T) {
	t.Run("empty slice returns empty JSON array", func(t *testing.T) {
		got := encodeAgentVariables([]agent.AgentVariable{})
		if got != "[]" {
			t.Errorf("encodeAgentVariables([]) = %q, want %q", got, "[]")
		}
	})

	t.Run("nil slice returns empty JSON array", func(t *testing.T) {
		got := encodeAgentVariables(nil)
		if got != "[]" {
			t.Errorf("encodeAgentVariables(nil) = %q, want %q", got, "[]")
		}
	})

	t.Run("populated slice returns valid JSON", func(t *testing.T) {
		vars := []agent.AgentVariable{
			{Name: "lead_name", Description: "Lead name", DefaultValue: "Cliente"},
			{Name: "plan_tier", DefaultValue: "basic"},
		}
		got := encodeAgentVariables(vars)
		if got == "" || got == "[]" {
			t.Errorf("encodeAgentVariables(populated) = %q, want non-empty JSON", got)
		}

		decoded := decodeAgentVariables(got)
		if len(decoded) != 2 {
			t.Fatalf("round-trip: expected 2 variables, got %d", len(decoded))
		}
		if decoded[0].Name != "lead_name" {
			t.Errorf("round-trip: decoded[0].Name = %q, want %q", decoded[0].Name, "lead_name")
		}
		if decoded[0].Description != "Lead name" {
			t.Errorf("round-trip: decoded[0].Description = %q, want %q", decoded[0].Description, "Lead name")
		}
		if decoded[0].DefaultValue != "Cliente" {
			t.Errorf("round-trip: decoded[0].DefaultValue = %q, want %q", decoded[0].DefaultValue, "Cliente")
		}
		if decoded[1].Name != "plan_tier" {
			t.Errorf("round-trip: decoded[1].Name = %q, want %q", decoded[1].Name, "plan_tier")
		}
		if decoded[1].Description != "" {
			t.Errorf("round-trip: decoded[1].Description = %q, want empty", decoded[1].Description)
		}
		if decoded[1].DefaultValue != "basic" {
			t.Errorf("round-trip: decoded[1].DefaultValue = %q, want %q", decoded[1].DefaultValue, "basic")
		}
	})
}

func TestDecodeAgentVariables(t *testing.T) {
	t.Run("empty string returns nil", func(t *testing.T) {
		got := decodeAgentVariables("")
		if got != nil {
			t.Errorf("decodeAgentVariables('') = %v, want nil", got)
		}
	})

	t.Run("whitespace string returns nil", func(t *testing.T) {
		got := decodeAgentVariables("   ")
		if got != nil {
			t.Errorf("decodeAgentVariables('   ') = %v, want nil", got)
		}
	})

	t.Run("empty JSON array returns empty slice", func(t *testing.T) {
		got := decodeAgentVariables("[]")
		if got == nil {
			t.Error("decodeAgentVariables('[]') = nil, want empty slice")
		}
		if len(got) != 0 {
			t.Errorf("decodeAgentVariables('[]') = %d items, want 0", len(got))
		}
	})

	t.Run("invalid JSON returns nil", func(t *testing.T) {
		got := decodeAgentVariables("{not json}")
		if got != nil {
			t.Errorf("decodeAgentVariables('{not json}') = %v, want nil", got)
		}
	})

	t.Run("null string returns nil", func(t *testing.T) {
		got := decodeAgentVariables("null")
		if got != nil {
			t.Errorf("decodeAgentVariables('null') = %v, want nil", got)
		}
	})

	t.Run("round-trip preserves all fields", func(t *testing.T) {
		vars := []agent.AgentVariable{
			{Name: "company", Description: "The company", DefaultValue: "Acme"},
			{Name: "plan"},
		}
		encoded := encodeAgentVariables(vars)
		decoded := decodeAgentVariables(encoded)
		if len(decoded) != 2 {
			t.Fatalf("round-trip: expected 2, got %d", len(decoded))
		}
		if decoded[0] != vars[0] {
			t.Errorf("round-trip: decoded[0] = %+v, want %+v", decoded[0], vars[0])
		}
		if decoded[1] != vars[1] {
			t.Errorf("round-trip: decoded[1] = %+v, want %+v", decoded[1], vars[1])
		}
	})
}
