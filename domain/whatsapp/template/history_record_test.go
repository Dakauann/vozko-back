package template

import (
	"encoding/json"
	"testing"
)

func TestTheHistoryRecordOfASendKeepsTheRenderedBodyAndTheTemplateFacts(t *testing.T) {
	tmpl := &Template{
		Name:     "promo_outubro",
		Language: "pt_BR",
		Category: TemplateCategoryMarketing,
		Components: []TemplateComponent{
			{Type: "BODY", Text: "Oi {{1}}, ofertas da semana."},
			{Type: "BUTTONS", Buttons: []TemplateButton{{Type: "QUICK_REPLY", Text: "Quero"}, {Type: "QUICK_REPLY", Text: "SAIR"}}},
		},
	}

	body, metadata, err := tmpl.HistoryRecord([]string{"Ana"})
	if err != nil {
		t.Fatal(err)
	}

	if body != "Oi Ana, ofertas da semana." {
		t.Fatalf("body = %q", body)
	}
	var facts map[string]interface{}
	if err := json.Unmarshal(metadata, &facts); err != nil {
		t.Fatal(err)
	}
	if facts["template_name"] != "promo_outubro" || facts["language"] != "pt_BR" || facts["body_text"] != body || facts["components"] == nil {
		t.Fatalf("metadata = %s", metadata)
	}
}
