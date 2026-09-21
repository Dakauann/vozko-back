package template

import "testing"

func namedBody() TemplateComponent {
	return TemplateComponent{Type: "BODY", Text: "Cartão de Todos {{unidade}}\n\nOlá, {{nome}} - {{matricula}}"}
}

func positionalBody() TemplateComponent {
	return TemplateComponent{Type: "BODY", Text: "Olá {{1}}, sua matrícula {{2}}"}
}

func TestIsNamedParameterFormat_BodyIsSourceOfTruth(t *testing.T) {
	cases := []struct {
		name string
		tmpl Template
		want bool
	}{
		{"named body, stored positional (the bug)", Template{ParameterFormat: ParameterFormatPositional, Components: []TemplateComponent{namedBody()}}, true},
		{"named body, stored named", Template{ParameterFormat: ParameterFormatNamed, Components: []TemplateComponent{namedBody()}}, true},
		{"named body, stored empty", Template{ParameterFormat: "", Components: []TemplateComponent{namedBody()}}, true},
		{"positional body, stored positional", Template{ParameterFormat: ParameterFormatPositional, Components: []TemplateComponent{positionalBody()}}, false},
		{"positional body, stored empty", Template{ParameterFormat: "", Components: []TemplateComponent{positionalBody()}}, false},
		{"no params, stored positional", Template{ParameterFormat: ParameterFormatPositional, Components: []TemplateComponent{{Type: "BODY", Text: "no vars here"}}}, false},
	}
	for _, tc := range cases {
		if got := tc.tmpl.IsNamedParameterFormat(); got != tc.want {
			t.Errorf("%s: IsNamedParameterFormat() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
