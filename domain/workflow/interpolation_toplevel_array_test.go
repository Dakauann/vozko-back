package workflow

import "testing"

// Reproduces the production bug on workflow node s2_2: an HTTP node captured a
// Supabase REST response whose BODY IS A JSON ARRAY, `[{"id":1,"token":"..."}]`,
// into a variable, then referenced the token as `{{token_consulta_cadastro[0].token}}`.
//
// The reference resolved to the literal unresolved string, producing an empty
// `Authorization: Bearer ` header and a 401 from the downstream API, even after
// the operator corrected the path and removed a duplicate "Bearer" prefix.
//
// Root cause (resolver half): a TOP-LEVEL array captured variable hits the
// `default` scope, which only deep-resolved `map[string]interface{}` and ignored
// `[]interface{}`. The existing TestInterpolate_ArrayIndexing only covers a map
// that *wraps* an array (`resposta.data[0]`), so this class went uncaught.
func TestInterpolate_TopLevelArrayVariable(t *testing.T) {
	state := NewRunState()
	// Exactly what a JSON array response body parses into.
	state.Set("token_consulta_cadastro", []interface{}{
		map[string]interface{}{"id": float64(1), "token": "3656101BC0B3C37D"},
		map[string]interface{}{"id": float64(2), "token": "SECOND"},
	})

	cases := map[string]string{
		"{{token_consulta_cadastro[0].token}}":        "3656101BC0B3C37D",
		"{{token_consulta_cadastro[1].token}}":        "SECOND",
		"{{token_consulta_cadastro[0].id}}":           "1",
		"Bearer {{token_consulta_cadastro[0].token}}": "Bearer 3656101BC0B3C37D",
		"{{ token_consulta_cadastro[0].token }}":      "3656101BC0B3C37D", // whitespace tolerant
		// out of range stays a literal (unchanged) rather than erroring
		"{{token_consulta_cadastro[9].token}}": "{{token_consulta_cadastro[9].token}}",
	}
	for tmpl, want := range cases {
		if got := Interpolate(tmpl, &state, nil); got != want {
			t.Errorf("Interpolate(%q) = %q, want %q", tmpl, got, want)
		}
	}
}

// ResolveVariable (used by nodes that need the typed value, not a string) must
// also index into a top-level array variable.
func TestResolveVariable_TopLevelArray(t *testing.T) {
	state := NewRunState()
	state.Set("tokens", []interface{}{
		map[string]interface{}{"token": "AAA"},
		map[string]interface{}{"token": "BBB"},
	})
	if v, ok := ResolveVariable("tokens[1].token", &state); !ok || v != "BBB" {
		t.Fatalf("ResolveVariable(tokens[1].token) = %v (ok=%v), want BBB", v, ok)
	}
}
