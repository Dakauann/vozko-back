package template

import (
	"testing"

	"vozko/domain/conversation"
)

func quickReplyTemplate(buttons ...TemplateButton) *Template {
	return &Template{
		Name:     "promo_outubro",
		Language: "pt_BR",
		Category: TemplateCategoryMarketing,
		Components: []TemplateComponent{
			{Type: "BODY", Text: "Ofertas da semana."},
			{Type: "BUTTONS", Buttons: buttons},
		},
	}
}

func TestQuickRepliesListsOnlyQuickReplyButtonsWithTheirOwnIndex(t *testing.T) {
	tmpl := quickReplyTemplate(
		TemplateButton{Type: "QUICK_REPLY", Text: "Quero saber mais"},
		TemplateButton{Type: "quick_reply", Text: " SAIR "},
		TemplateButton{Type: "URL", Text: "Ver loja", URL: "https://loja.exemplo.com"},
	)

	got := tmpl.QuickReplies()

	want := []QuickReply{{Index: 0, Text: "Quero saber mais"}, {Index: 1, Text: "SAIR"}}
	if len(got) != len(want) {
		t.Fatalf("quick replies = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("quick replies = %+v, want %+v", got, want)
		}
	}
}

func TestQuickRepliesOfATemplateWithoutButtonsIsEmpty(t *testing.T) {
	tmpl := &Template{Components: []TemplateComponent{{Type: "BODY", Text: "Oi"}}}
	if got := tmpl.QuickReplies(); len(got) != 0 {
		t.Fatalf("quick replies = %+v, want none", got)
	}
	var none *Template
	if got := none.QuickReplies(); len(got) != 0 {
		t.Fatalf("a nil template has no quick replies, got %+v", got)
	}
}

func TestQuickReplyPayloadRoundTrips(t *testing.T) {
	for _, index := range []int{0, 1, 9} {
		payload := QuickReplyPayload(index)
		got, ok := ParseQuickReplyPayload(payload)
		if !ok || got != index {
			t.Fatalf("%q parsed as %d, %v; want %d", payload, got, ok, index)
		}
	}
	if QuickReplyPayload(2) != "vz1:2" {
		t.Fatalf("payload = %q, want vz1:2", QuickReplyPayload(2))
	}
}

func TestParseQuickReplyPayloadRefusesAnythingElse(t *testing.T) {
	for _, payload := range []string{"", "SAIR", "vz1:", "vz1:-1", "vz1:10", "vz1:a", "vz2:1", "vz1:1:2", " vz1:1", "vz1:01"} {
		if index, ok := ParseQuickReplyPayload(payload); ok {
			t.Errorf("%q parsed as %d, want no match", payload, index)
		}
	}
}

func TestBuildSendInputCarriesAPayloadForEveryQuickReply(t *testing.T) {
	tmpl := quickReplyTemplate(
		TemplateButton{Type: "QUICK_REPLY", Text: "Falar com atendente"},
		TemplateButton{Type: "QUICK_REPLY", Text: "SAIR"},
	)

	out, err := tmpl.BuildSendInput(SendInputParams{To: "5511987654321"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []conversation.TemplateButtonParam{
		{SubType: conversation.TemplateButtonSubTypeQuickReply, Index: 0, Payload: "vz1:0"},
		{SubType: conversation.TemplateButtonSubTypeQuickReply, Index: 1, Payload: "vz1:1"},
	}
	if len(out.Buttons) != len(want) {
		t.Fatalf("buttons = %+v, want %+v", out.Buttons, want)
	}
	for i := range want {
		if out.Buttons[i] != want[i] {
			t.Fatalf("buttons = %+v, want %+v", out.Buttons, want)
		}
	}
}
