package template

import (
	"errors"
	"testing"
)

func TestCheckParameterNameFollowsMetasNamedVariableRule(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{name: "primeiro_nome", ok: true},
		{name: "pedido2", ok: true},
		{name: "a", ok: true},
		{name: "Nome", ok: false},
		{name: "_nome", ok: false},
		{name: "2pedido", ok: false},
		{name: "nome completo", ok: false},
		{name: "série", ok: false},
		{name: "nome-completo", ok: false},
		{name: "", ok: false},
	}
	for _, tc := range cases {
		err := CheckParameterName(tc.name)
		if tc.ok != (err == nil) {
			t.Fatalf("CheckParameterName(%q) = %v", tc.name, err)
		}
		var named *ParameterNameError
		if !tc.ok && (!errors.Is(err, ErrParameterNameInvalid) || !errors.As(err, &named) || named.Name != tc.name) {
			t.Fatalf("%q refusal = %v", tc.name, err)
		}
	}
}

func TestADraftWithAnInvalidNamedVariableIsRefusedNamingIt(t *testing.T) {
	components := []TemplateComponent{
		{Type: "HEADER", Format: "TEXT", Text: "Loja {{loja}}", Example: &TemplateExample{HeaderTextNamed: []NamedParamExample{{ParamName: "loja", Example: "Prisma"}}}},
		{Type: "BODY", Text: "Olá {{primeiro_nome}}, pedido {{Pedido}} saiu.", Example: &TemplateExample{BodyTextNamed: []NamedParamExample{{ParamName: "primeiro_nome", Example: "Ana"}, {ParamName: "Pedido", Example: "77"}}}},
	}
	err := ValidateDraft("pedido_saiu", TemplateCategoryUtility, components)
	var named *ParameterNameError
	if !errors.As(err, &named) || named.Name != "Pedido" || ErrorCode(err) != CodeParameterNameInvalid || !IsValidationError(err) {
		t.Fatalf("ValidateDraft = %v", err)
	}
	components[1].Text = "Olá {{primeiro_nome}}, pedido {{pedido}} saiu."
	components[1].Example.BodyTextNamed[1].ParamName = "pedido"
	if err := ValidateDraft("pedido_saiu", TemplateCategoryUtility, components); err != nil {
		t.Fatalf("a valid named draft = %v", err)
	}
	positional := []TemplateComponent{{Type: "BODY", Text: "Olá {{1}}, pedido {{2}} saiu.", Example: &TemplateExample{BodyText: [][]string{{"Ana", "77"}}}}}
	if err := ValidateDraft("pedido_saiu", TemplateCategoryUtility, positional); err != nil {
		t.Fatalf("a numbered draft = %v", err)
	}
}

func TestCheckParameterNamesStopsAtTheFirstInvalidName(t *testing.T) {
	if err := CheckParameterNames([]string{"nome", "pedido_id"}); err != nil {
		t.Fatalf("valid names = %v", err)
	}
	var named *ParameterNameError
	if err := CheckParameterNames([]string{"nome", "Bairro", "x y"}); !errors.As(err, &named) || named.Name != "Bairro" {
		t.Fatalf("first invalid = %v", err)
	}
}
