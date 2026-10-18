package buttonaction

import (
	"slices"
	"testing"

	"vozko/domain/whatsapp/template"
)

func templateWithText(body, footer string) *template.Template {
	components := []template.TemplateComponent{{Type: "BODY", Text: body}}
	if footer != "" {
		components = append(components, template.TemplateComponent{Type: "FOOTER", Text: footer})
	}
	return &template.Template{Components: components}
}

func TestExitWordsAreReadFromWhatTheLeadSees(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		footer string
		want   []string
	}{
		{"footer command", "Oferta da semana", "Responda /sair para não receber mais.", []string{"sair"}},
		{"quoted command", "Envie \"/PARAR\" para parar", "", []string{"PARAR"}},
		{"bracketed command", "Promoção (/cancelar)", "", []string{"cancelar"}},
		{"accented command", "", "Responda /Saír", []string{"Saír"}},
		{"several commands, repeats folded", "Use /sair ou /parar", "Responda /SAIR", []string{"sair", "parar"}},
		{"command at the start", "/sair para sair", "", []string{"sair"}},
		{"a url is not a command", "Veja https://loja.com/promo e aproveite", "", []string{}},
		{"a date is not a command", "Válido até 10/11", "", []string{}},
		{"no command", "Olá {{1}}", "Equipe Vozko", []string{}},
	}
	for _, tc := range cases {
		got := ShownExitWords(templateWithText(tc.body, tc.footer))
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestEveryVisibleTextCanShowAnExitWord(t *testing.T) {
	tmpl := &template.Template{Components: []template.TemplateComponent{
		{Type: "HEADER", Format: "TEXT", Text: "Aviso /sair"},
		{Type: "BODY", Text: "Oi"},
		{Type: "BUTTONS", Buttons: []template.TemplateButton{{Type: "QUICK_REPLY", Text: "/parar"}, {Type: "URL", Text: "Abrir", URL: "https://loja.com/a"}}},
	}}
	if got := ShownExitWords(tmpl); !slices.Equal(got, []string{"sair", "parar"}) {
		t.Fatalf("got %q", got)
	}
}

func TestAMediaHeaderHandleIsNotText(t *testing.T) {
	tmpl := &template.Template{Components: []template.TemplateComponent{
		{Type: "HEADER", Format: "IMAGE", Example: &template.TemplateExample{HeaderHandle: []string{"https://cdn.example.com/x /sair"}}},
	}}
	if got := ShownExitWords(tmpl); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestANilTemplateShowsNoExitWords(t *testing.T) {
	if got := ShownExitWords(nil); got == nil || len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestAPlainMessageShowsTheExitWordsItsTextNames(t *testing.T) {
	got := ExitWordsShownIn("Oi Ana, promoção da semana.", "Responda /sair para não receber mais.")
	if !slices.Equal(got, []string{"sair"}) {
		t.Fatalf("got %q", got)
	}
}
