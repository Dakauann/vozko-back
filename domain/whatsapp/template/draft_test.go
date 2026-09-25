package template

import (
	"errors"
	"testing"
)

func body(text string, examples ...string) TemplateComponent {
	c := TemplateComponent{Type: "BODY", Text: text}
	if len(examples) > 0 {
		c.Example = &TemplateExample{BodyText: [][]string{examples}}
	}
	return c
}

func TestValidateDraftAcceptsAMetaReadyTemplate(t *testing.T) {
	if err := ValidateDraft("refiliacao_convite", TemplateCategoryMarketing, []TemplateComponent{body("Olá {{1}}, volte!", "Maria")}); err != nil {
		t.Fatalf("valid draft refused: %v", err)
	}
}

func TestValidateDraftRefusesWhatMetaWouldReject(t *testing.T) {
	cases := map[string]struct {
		name       string
		category   TemplateCategory
		components []TemplateComponent
	}{
		"name with spaces":      {"Refiliação Convite", TemplateCategoryMarketing, []TemplateComponent{body("Olá")}},
		"unknown category":      {"convite", TemplateCategory("PROMO"), []TemplateComponent{body("Olá")}},
		"variable with no body": {"convite", TemplateCategoryMarketing, []TemplateComponent{body("Olá {{1}}")}},
		"no body":               {"convite", TemplateCategoryMarketing, nil},
	}
	for label, c := range cases {
		if err := ValidateDraft(c.name, c.category, c.components); err == nil {
			t.Errorf("%s: accepted", label)
		}
	}
	if err := ValidateDraft("convite", TemplateCategory("PROMO"), []TemplateComponent{body("Olá")}); !errors.Is(err, ErrInvalidCategory) {
		t.Fatalf("category error = %v", err)
	}
}
