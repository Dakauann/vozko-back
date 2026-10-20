package sqlexpr

import "testing"

func TestAnAgentBindingFilterMatchesTheStoredBindingsShape(t *testing.T) {
	want := `a.tool_bindings @> CAST('{"bindings":[{"name":"run_workflow"}]}' AS jsonb)`
	if got := AgentBindsTool("a", "run_workflow"); got != want {
		t.Fatalf("AgentBindsTool = %q\nwant           %q", got, want)
	}
}
