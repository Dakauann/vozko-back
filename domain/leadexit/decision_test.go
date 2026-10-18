package leadexit

import (
	"testing"

	"vozko/domain/buttonaction"
)

var promoRef = buttonaction.TemplateRef{Name: "promo_outubro", Language: "pt_BR"}

func optOutAt(index int, confirmation string) *buttonaction.ButtonAction {
	return &buttonaction.ButtonAction{Template: promoRef, ButtonIndex: index, ButtonText: "SAIR", Kind: buttonaction.KindOptOut, Confirmation: confirmation}
}

func handOffAt(index int, confirmation string) *buttonaction.ButtonAction {
	return &buttonaction.ButtonAction{Template: promoRef, ButtonIndex: index, ButtonText: "Falar com atendente", Kind: buttonaction.KindHandOff,
		Params: buttonaction.Params{DepartmentID: "dep-1"}, Confirmation: confirmation}
}

func resolved(action *buttonaction.ButtonAction) *buttonaction.Resolution {
	return &buttonaction.Resolution{Template: promoRef, ButtonIndex: action.ButtonIndex, Action: action, Source: buttonaction.SourceTemplate, Reason: buttonaction.ReasonResolved}
}

func TestATappedButtonActionWinsOverEveryWord(t *testing.T) {
	d, ok := Decide(Candidates{
		Text:      "sair",
		Tap:       resolved(handOffAt(0, "Já chamamos alguém.")),
		Templates: []TemplateWords{{Template: promoRef, Words: []string{"sair"}}},
	})
	if !ok || d.Trigger != TriggerButton || d.Kind != buttonaction.KindHandOff || d.Action == nil || d.Confirmation != "Já chamamos alguém." {
		t.Fatalf("got %+v, %v", d, ok)
	}
}

func TestATapWithoutAnActionFallsBackToTheWords(t *testing.T) {
	d, ok := Decide(Candidates{
		Text:      "/SAIR",
		Tap:       &buttonaction.Resolution{Template: promoRef, ButtonIndex: 1, Reason: buttonaction.ReasonNoAction},
		Templates: []TemplateWords{{Template: promoRef, Words: []string{"sair"}, Confirmation: "Você não receberá mais mensagens."}},
	})
	if !ok || d.Trigger != TriggerTemplateWord || d.Kind != buttonaction.KindOptOut || d.Word != "sair" || d.Confirmation != "Você não receberá mais mensagens." {
		t.Fatalf("got %+v, %v", d, ok)
	}
}

func TestAShownWordMatchesOnlyItsFoldedSlashCommand(t *testing.T) {
	shown := []TemplateWords{{Template: promoRef, Words: []string{"sair"}}}
	for _, text := range []string{"/Sair", " /sair ", "/SAIR", "/SAÍR"} {
		if d, ok := Decide(Candidates{Text: text, Templates: shown}); !ok || d.Kind != buttonaction.KindOptOut || d.Trigger != TriggerTemplateWord {
			t.Errorf("%q: got %+v, %v", text, d, ok)
		}
	}
	for _, text := range []string{"sair", "Sair", "SAIR", "quero sair do plano", "/sair?", "não quero /sair", "/ sair", ""} {
		if d, ok := Decide(Candidates{Text: text, Templates: shown}); ok {
			t.Errorf("%q: got %+v, want no exit", text, d)
		}
	}
}

func TestNoShownWordsMeanNoWordExit(t *testing.T) {
	for _, text := range []string{"sair", "SAIR", "/sair", "stop", "parar"} {
		if d, ok := Decide(Candidates{Text: text}); ok {
			t.Errorf("%q: got %+v, want nothing", text, d)
		}
	}
}

func TestTemplateWordsCountOnlyForTheTemplatesTheLeadReceived(t *testing.T) {
	if d, ok := Decide(Candidates{Text: "/sair"}); ok {
		t.Fatalf("no template received: got %+v", d)
	}
	other := TemplateWords{Template: buttonaction.TemplateRef{Name: "boleto", Language: "pt_BR"}, Words: []string{"/cancelar"}}
	if d, ok := Decide(Candidates{Text: "/sair", Templates: []TemplateWords{other}}); ok {
		t.Fatalf("another template's word: got %+v", d)
	}
}

func TestAnOptOutTapWithoutItsOwnConfirmationUsesTheTemplateOne(t *testing.T) {
	tapped := TemplateWords{Template: promoRef, Confirmation: "Confirmação do modelo."}
	cases := []struct {
		name      string
		action    *buttonaction.ButtonAction
		templates []TemplateWords
		want      string
	}{
		{"the button's own", optOutAt(1, "Do botão."), []TemplateWords{tapped}, "Do botão."},
		{"the template's", optOutAt(1, ""), []TemplateWords{tapped}, "Confirmação do modelo."},
		{"none configured", optOutAt(1, ""), nil, ""},
	}
	for _, tc := range cases {
		d, ok := Decide(Candidates{Text: "SAIR", Tap: resolved(tc.action), Templates: tc.templates})
		if !ok || d.Confirmation != tc.want {
			t.Errorf("%s: got %q (%v), want %q", tc.name, d.Confirmation, ok, tc.want)
		}
	}
}

func TestANonExitTapSendsOnlyTheButtonsOwnConfirmation(t *testing.T) {
	d, ok := Decide(Candidates{
		Text:      "Falar com atendente",
		Tap:       resolved(handOffAt(0, "")),
		Templates: []TemplateWords{{Template: promoRef, Confirmation: "Confirmação do modelo."}},
	})
	if !ok || d.Confirmation != "" {
		t.Fatalf("got %+v, %v", d, ok)
	}
}

func TestAStaleOrUnmatchedTapRunsNoAction(t *testing.T) {
	for _, reason := range []buttonaction.Reason{buttonaction.ReasonStale, buttonaction.ReasonNoMatch, buttonaction.ReasonTemplateNotFound} {
		if d, ok := Decide(Candidates{Text: "Falar com atendente", Tap: &buttonaction.Resolution{Template: promoRef, Reason: reason}}); ok {
			t.Errorf("%s: got %+v", reason, d)
		}
	}
}

func TestATapMatchedAButtonOnlyWhenTheTemplateHadIt(t *testing.T) {
	cases := map[buttonaction.Reason]bool{
		buttonaction.ReasonResolved:          true,
		buttonaction.ReasonNoAction:          true,
		buttonaction.ReasonStale:             true,
		buttonaction.ReasonNoMatch:           false,
		buttonaction.ReasonTemplateNotFound:  false,
		buttonaction.ReasonTemplateAmbiguous: false,
	}
	for reason, want := range cases {
		if got := ButtonMatched(&buttonaction.Resolution{Reason: reason}); got != want {
			t.Errorf("%s: got %v, want %v", reason, got, want)
		}
	}
	if ButtonMatched(nil) {
		t.Fatal("nil resolution matched")
	}
}

func campaignShowing(text string) *SentCampaignMessage {
	return &SentCampaignMessage{MessageID: "msg-camp", CampaignID: "camp-u", Text: text}
}

func TestAWordShownInTheLastCampaignMessageOptsTheLeadOut(t *testing.T) {
	d, ok := Decide(Candidates{Text: "/SAIR", Campaign: campaignShowing("Oi Ana! Responda /sair para não receber mais mensagens.")})
	if !ok || d.Trigger != TriggerCampaignWord || d.Kind != buttonaction.KindOptOut || d.Word != "sair" || d.CampaignID != "camp-u" {
		t.Fatalf("got %+v, %v", d, ok)
	}
	if d.Confirmation != "" || d.Action != nil || !d.Template.Empty() {
		t.Fatalf("a campaign word has no confirmation, action or template: %+v", d)
	}
}

func TestOnlyTheCommandTheCampaignMessageShowsCounts(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		shown string
	}{
		{"another command", "/parar", "Responda /sair para não receber mais mensagens."},
		{"a plain word", "sair", "Responda /sair para não receber mais mensagens."},
		{"a link path", "/sair", "Veja https://loja.com/sair e aproveite."},
		{"a date", "/10", "Promoção até 12/10."},
		{"no command at all", "/sair", "Oi Ana, tudo bem?"},
	}
	for _, tc := range cases {
		if d, ok := Decide(Candidates{Text: tc.text, Campaign: campaignShowing(tc.shown)}); ok {
			t.Errorf("%s: got %+v, want no exit", tc.name, d)
		}
	}
}

func TestATemplateWordIsCheckedBeforeTheCampaignMessage(t *testing.T) {
	d, ok := Decide(Candidates{
		Text:      "/sair",
		Templates: []TemplateWords{{Template: promoRef, Words: []string{"sair"}, Confirmation: "Do modelo."}},
		Campaign:  campaignShowing("Responda /sair"),
	})
	if !ok || d.Trigger != TriggerTemplateWord || d.Confirmation != "Do modelo." || d.CampaignID != "" {
		t.Fatalf("got %+v, %v", d, ok)
	}
}
