package workflow

import "testing"

func TestInterpolate_TopLevelArrayVariable(t *testing.T) {
	state := NewRunState()
	state.Set("token_consulta_cadastro", []interface{}{
		map[string]interface{}{"id": float64(1), "token": "3656101BC0B3C37D"},
		map[string]interface{}{"id": float64(2), "token": "SECOND"},
	})

	cases := map[string]string{
		"{{token_consulta_cadastro[0].token}}":        "3656101BC0B3C37D",
		"{{token_consulta_cadastro[1].token}}":        "SECOND",
		"{{token_consulta_cadastro[0].id}}":           "1",
		"Bearer {{token_consulta_cadastro[0].token}}": "Bearer 3656101BC0B3C37D",
		"{{ token_consulta_cadastro[0].token }}":      "3656101BC0B3C37D",
		"{{token_consulta_cadastro[9].token}}":        "{{token_consulta_cadastro[9].token}}",
	}
	for tmpl, want := range cases {
		if got := Interpolate(tmpl, &state, nil); got != want {
			t.Errorf("Interpolate(%q) = %q, want %q", tmpl, got, want)
		}
	}
}

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
