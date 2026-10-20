package workflow

import (
	"reflect"
	"testing"
)

func TestLookupResolvesTheSamePathsTemplatesDoSoCodeAndTemplatesAgree(t *testing.T) {
	state := NewRunState()
	RecordNodeOutput(&state, "n55", map[string]interface{}{
		"status_code": 200,
		"body":        map[string]interface{}{"accessToken": "x"},
	})
	state.Set("ai_response", map[string]interface{}{
		"tool_args": map[string]interface{}{"query": "eleição 2026"},
	})
	state.Set("hits", []interface{}{map[string]interface{}{"title": "first"}})
	state.Set("counter", 3)

	cases := []struct {
		path string
		want interface{}
	}{
		{"node.n55.status_code", 200},
		{"node.n55.body.accessToken", "x"},
		{"last.status_code", 200},
		{"last.body.accessToken", "x"},
		{"_node_n55_status_code", 200},
		{"counter", 3},
		{"var.counter", 3},
		{"ai_response.tool_args.query", "eleição 2026"},
		{"hits.0.title", "first"},
		{"hits[0].title", "first"},
	}
	for _, c := range cases {
		got, ok := state.Lookup(c.path)
		if !ok || !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%s: got %v (%v), want %v", c.path, got, ok, c.want)
		}
		rendered := Interpolate("{{"+c.path+"}}", &state, nil)
		if rendered != TemplateText(c.want) {
			t.Fatalf("%s: template rendered %q, lookup gave %v", c.path, rendered, c.want)
		}
	}
}

func TestLookupMissesStayMissesSoScriptsSeeUndefined(t *testing.T) {
	state := NewRunState()
	state.Set("ai_response", map[string]interface{}{"tool_args": map[string]interface{}{"query": "q"}})
	state.Set("hits", []interface{}{map[string]interface{}{"title": "first"}})

	for _, path := range []string{
		"ai_response.tool_args.missing",
		"ai_response.tool_args.query.notObject",
		"hits.5.title",
		"absent.key",
		"absent",
		"node.n55.status_code",
		"last.status_code",
	} {
		if got, ok := state.Lookup(path); ok {
			t.Fatalf("%s: expected a miss, got %v", path, got)
		}
	}
}

func TestLookupPrefersALiteralDottedKeyBecauseScriptsAlreadyRelyOnIt(t *testing.T) {
	state := NewRunState()
	state.Set("ai_response.tool_args.cep", "01001-000")
	state.Set("ai_response", map[string]interface{}{"tool_args": map[string]interface{}{"cep": "should-not-win"}})

	got, ok := state.Lookup("ai_response.tool_args.cep")
	if !ok || got != "01001-000" {
		t.Fatalf("got %v (%v)", got, ok)
	}
}

func TestLookupOnANilStateIsAMiss(t *testing.T) {
	var state *RunState
	if _, ok := state.Lookup("anything"); ok {
		t.Fatal("expected a miss")
	}
}
