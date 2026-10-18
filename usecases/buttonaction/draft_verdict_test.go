package buttonaction_usecase

import (
	"errors"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
)

func draftButtons(texts ...string) []template.TemplateButton {
	out := make([]template.TemplateButton, 0, len(texts))
	for _, text := range texts {
		out = append(out, template.TemplateButton{Type: "QUICK_REPLY", Text: text})
	}
	return out
}

func TestTheDraftVerdictComesFromTheDraftAlone(t *testing.T) {
	marketing := template.TemplateCategoryMarketing
	cases := []struct {
		name         string
		draft        DraftTemplate
		state        buttonaction.VerdictState
		exitRequired bool
	}{
		{"a marketing draft with no exit", DraftTemplate{Category: marketing, Body: "Ofertas", Buttons: draftButtons("Quero ver")}, buttonaction.VerdictNone, true},
		{"an exit action on a draft button", DraftTemplate{Category: marketing, Buttons: draftButtons("Quero ver", "Parar"),
			Actions: []buttonaction.Input{{ButtonIndex: 1, ButtonText: "Parar", Kind: buttonaction.KindOptOut}}}, buttonaction.VerdictOK, false},
		{"a body that shows a word", DraftTemplate{Category: marketing, Body: "Para cancelar, digite /sair."}, buttonaction.VerdictOK, false},
		{"a footer that shows a word", DraftTemplate{Category: marketing, Body: "Ofertas", Footer: "Responda /parar"}, buttonaction.VerdictOK, false},
		{"a text header that shows a word", DraftTemplate{Category: marketing, Header: "Aviso /sair", Body: "Ofertas"}, buttonaction.VerdictOK, false},
		{"a draft button that is a command", DraftTemplate{Category: marketing, Buttons: draftButtons("/Sair")}, buttonaction.VerdictOK, false},
		{"a footer naming a word without the slash", DraftTemplate{Category: marketing, Footer: "Responda SAIR", Buttons: draftButtons("Sair")}, buttonaction.VerdictNone, true},
		{"a utility draft with no exit", DraftTemplate{Category: template.TemplateCategoryUtility, Buttons: draftButtons("Confirmar")}, buttonaction.VerdictNone, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			view, err := f.svc.DraftExitVerdict(ctx, actor(), tc.draft)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if view.Verdict.State != tc.state || view.Gates.TemplateExitRequired != tc.exitRequired {
				t.Fatalf("verdict = %s, exit required %v; want %s, %v", view.Verdict.State, view.Gates.TemplateExitRequired, tc.state, tc.exitRequired)
			}
			if f.settings.saves != 0 || len(f.routes.rows) != 0 {
				t.Fatalf("a draft verdict stored something")
			}
		})
	}
}

func TestTheDraftVerdictRefusesWhatSavingWouldRefuse(t *testing.T) {
	f := newFixture(t)
	cases := map[string]DraftTemplate{
		"an unknown kind":     {Buttons: draftButtons("Parar"), Actions: []buttonaction.Input{{ButtonIndex: 0, ButtonText: "Parar", Kind: "send_template"}}},
		"a missing button":    {Buttons: draftButtons("Parar"), Actions: []buttonaction.Input{{ButtonIndex: 3, ButtonText: "Parar", Kind: buttonaction.KindOptOut}}},
		"two on one button":   {Buttons: draftButtons("Parar"), Actions: []buttonaction.Input{{ButtonIndex: 0, ButtonText: "Parar", Kind: buttonaction.KindOptOut}, {ButtonIndex: 0, ButtonText: "Parar", Kind: buttonaction.KindOptOut}}},
		"a text not on draft": {Buttons: draftButtons("Parar"), Actions: []buttonaction.Input{{ButtonIndex: 0, ButtonText: "Sair", Kind: buttonaction.KindOptOut}}},
	}
	for name, draft := range cases {
		if _, err := f.svc.DraftExitVerdict(ctx, actor(), draft); buttonaction.ErrorCode(err) == "" {
			t.Errorf("%s: got %v, want a coded refusal", name, err)
		}
	}
	if _, err := f.svc.DraftExitVerdict(ctx, Actor{WorkspaceID: ws}, DraftTemplate{}); !errors.Is(err, buttonaction.ErrActorRequired) {
		t.Fatalf("without the person: %v", err)
	}
}
