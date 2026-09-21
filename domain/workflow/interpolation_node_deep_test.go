package workflow

import "testing"

func TestInterpolate_NodeScopeDeepAccess(t *testing.T) {
	rs := NewRunState()
	state := &rs
	state.Set("_node_n2_tool_args", map[string]interface{}{
		"cep":    "59255000",
		"nested": map[string]interface{}{"x": "y"},
	})
	state.Set("_node_n2_cep", "59255000")

	cases := []struct{ in, want string }{
		{"{{node.n2.cep}}", "59255000"},
		{"https://x/{{node.n2.tool_args.cep}}/json", "https://x/59255000/json"},
		{"{{node.n2.tool_args.nested.x}}", "y"},
		{"{{node.n2.tool_args.missing}}", "{{node.n2.tool_args.missing}}"},
		{"{{node.ghost.tool_args.cep}}", "{{node.ghost.tool_args.cep}}"},
	}
	for _, c := range cases {
		if got := Interpolate(c.in, state, nil); got != c.want {
			t.Fatalf("Interpolate(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	if v, ok := ResolveVariable("node.n2.tool_args.cep", state); !ok || v != "59255000" {
		t.Fatalf("ResolveVariable deep = %v, %v", v, ok)
	}
	if v, ok := ResolveVariable("node.n2.tool_args.nested.x", state); !ok || v != "y" {
		t.Fatalf("ResolveVariable nested deep = %v, %v", v, ok)
	}
	if _, ok := ResolveVariable("node.n2.tool_args.missing", state); ok {
		t.Fatal("ResolveVariable should fail for missing deep key")
	}
}
