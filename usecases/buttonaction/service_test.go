package buttonaction_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
)

var ctx = context.Background()

func optOutAndHandOff() TemplateChange {
	return TemplateChange{
		Actions: []buttonaction.Input{
			{ButtonIndex: 0, ButtonText: "Falar com atendente", Kind: buttonaction.KindHandOff, Params: buttonaction.Params{DepartmentID: "vendas"}},
			{ButtonIndex: 1, ButtonText: "SAIR", Kind: buttonaction.KindOptOut, Confirmation: "Entendido."},
		},
		ExitConfirmation: "Pronto.",
	}
}

func TestNewServiceRefusesMissingDependencies(t *testing.T) {
	if _, err := NewService(Deps{}); !errors.Is(err, buttonaction.ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
}

func TestSaveTemplateActionsStoresActionsAndTheExitConfirmation(t *testing.T) {
	f := newFixture(t)
	view, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.Settings.Version != 2 || len(view.Settings.Actions) != 2 || view.Settings.ExitConfirmation != "Pronto." || view.Settings.UpdatedBy != userID {
		t.Fatalf("view = %+v", view.Settings)
	}
	if view.Settings.Actions[1].ButtonText != "SAIR" || view.Buttons[1].Action == nil || view.Buttons[1].Action.Kind != buttonaction.KindOptOut {
		t.Fatalf("buttons = %+v", view.Buttons)
	}
}

func TestSaveTemplateActionsWithAStaleVersionConflicts(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, TemplateChange{}); !errors.Is(err, buttonaction.ErrVersionConflict) {
		t.Fatalf("got %v, want ErrVersionConflict", err)
	}
}

func TestSaveTemplateActionsNeedsTheCapabilityOfEachChangedKind(t *testing.T) {
	f := newFixture(t)
	change := TemplateChange{Actions: []buttonaction.Input{{ButtonIndex: 0, ButtonText: "Falar com atendente", Kind: buttonaction.KindStartWorkflow, Params: buttonaction.Params{WorkflowID: "wf-1"}}}}
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, change); !errors.Is(err, buttonaction.ErrForbidden) {
		t.Fatalf("workflow without workflows.edit: got %v, want ErrForbidden", err)
	}
	grant := TemplateChange{Actions: []buttonaction.Input{{ButtonIndex: 0, ButtonText: "Falar com atendente", Kind: buttonaction.KindConsentGrant, Params: buttonaction.Params{Purpose: "ofertas"}}}}
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, grant); !errors.Is(err, buttonaction.ErrForbidden) {
		t.Fatalf("consent grant without leads.consent: got %v, want ErrForbidden", err)
	}
	optOut := TemplateChange{Actions: []buttonaction.Input{{ButtonIndex: 1, ButtonText: "SAIR", Kind: buttonaction.KindOptOut}}}
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOut); err != nil {
		t.Fatalf("opt out needs only template management: %v", err)
	}
}

func TestKeepingAnActionDoesNotNeedItsCapabilityAgain(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); err != nil {
		t.Fatal(err)
	}
	delete(f.permission.granted, "leads:assign")
	checks := f.ownership.checks
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 2, optOutAndHandOff()); err != nil {
		t.Fatalf("unchanged hand off: %v", err)
	}
	if f.ownership.checks != checks {
		t.Fatalf("unchanged actions were checked again")
	}
	removeHandOff := optOutAndHandOff()
	removeHandOff.Actions = removeHandOff.Actions[1:]
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 3, removeHandOff); !errors.Is(err, buttonaction.ErrForbidden) {
		t.Fatalf("removing a hand off without leads.assign: got %v, want ErrForbidden", err)
	}
}

func TestSaveTemplateActionsRefusesParamsOutsideTheWorkspace(t *testing.T) {
	f := newFixture(t)
	f.ownership.refuse[buttonaction.KindHandOff] = true
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); !errors.Is(err, buttonaction.ErrParamsNotInWorkspace) {
		t.Fatalf("got %v, want ErrParamsNotInWorkspace", err)
	}
}

func TestTemplateActionsOfATemplateOutsideTheWorkspace(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.TemplateActions(ctx, actor(), "other"); !errors.Is(err, buttonaction.ErrTemplateNotFound) {
		t.Fatalf("got %v, want ErrTemplateNotFound", err)
	}
	if _, err := f.svc.TemplateActions(ctx, Actor{WorkspaceID: ws}, templateID); !errors.Is(err, buttonaction.ErrActorRequired) {
		t.Fatalf("got %v, want ErrActorRequired", err)
	}
}

func TestResolveTapByPayloadAndByTextAfterASave(t *testing.T) {
	f := newFixture(t)
	key := TemplateKey{WorkspaceID: ws, TemplateName: "promo_outubro", Language: "pt_BR"}

	before, err := f.svc.ResolveTap(ctx, TapInput{TemplateKey: key, Payload: "vz1:1", Text: "SAIR"})
	if err != nil || before.Action != nil || before.Reason != buttonaction.ReasonNoAction {
		t.Fatalf("before = %+v, %v", before, err)
	}
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); err != nil {
		t.Fatal(err)
	}

	byPayload, err := f.svc.ResolveTap(ctx, TapInput{TemplateKey: key, Payload: "vz1:1", Text: "SAIR"})
	if err != nil || byPayload.Action == nil || byPayload.Action.Kind != buttonaction.KindOptOut {
		t.Fatalf("by payload = %+v, %v", byPayload, err)
	}
	byText, err := f.svc.ResolveTap(ctx, TapInput{TemplateKey: TemplateKey{WorkspaceID: ws, TemplateID: templateID}, Payload: "SAIR", Text: "sair"})
	if err != nil || byText.Action == nil || byText.Action.Kind != buttonaction.KindOptOut {
		t.Fatalf("by text = %+v, %v", byText, err)
	}
	noMatch, err := f.svc.ResolveTap(ctx, TapInput{TemplateKey: key, Text: "Quero mais"})
	if err != nil || noMatch.Action != nil || noMatch.Reason != buttonaction.ReasonNoMatch {
		t.Fatalf("no match = %+v, %v", noMatch, err)
	}
}

func TestResolveTapOfAnUnknownTemplateIsNone(t *testing.T) {
	f := newFixture(t)
	got, err := f.svc.ResolveTap(ctx, TapInput{TemplateKey: TemplateKey{WorkspaceID: ws, TemplateName: "nao_existe", Language: "pt_BR"}, Payload: "vz1:0"})
	if err != nil || got.Reason != buttonaction.ReasonTemplateNotFound {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := f.svc.ResolveTap(ctx, TapInput{}); !errors.Is(err, buttonaction.ErrWorkspaceRequired) {
		t.Fatalf("got %v, want ErrWorkspaceRequired", err)
	}
}

func TestResolveTapRefusesTwoTemplatesWithTheSameNameAndDifferentButtons(t *testing.T) {
	f := newFixture(t)
	other := promoTemplate()
	other.ID = "tmpl-2"
	other.Components[1].Buttons[1].Text = "Parar"
	f.templates.byID[other.ID] = other
	got, err := f.svc.ResolveTap(ctx, TapInput{TemplateKey: TemplateKey{WorkspaceID: ws, TemplateName: "promo_outubro", Language: "pt_BR"}, Payload: "vz1:1"})
	if err != nil || got.Reason != buttonaction.ReasonTemplateAmbiguous {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestTemplateExitReadsTheWordsTheTemplateShowsAndTheStoredConfirmation(t *testing.T) {
	f := newFixture(t)
	f.templates.byID[templateID].Components[0].Text = "Ofertas da semana. Responda /parar para não receber."
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); err != nil {
		t.Fatal(err)
	}
	exit, err := f.svc.TemplateExit(ctx, TemplateKey{WorkspaceID: ws, TemplateID: templateID})
	if err != nil || len(exit.Words) != 1 || exit.Words[0] != "parar" || exit.Confirmation != "Pronto." {
		t.Fatalf("exit = %+v, %v", exit, err)
	}
}

func TestTemplateExitOfATemplateShowingNoWordHasNoWords(t *testing.T) {
	f := newFixture(t)
	exit, err := f.svc.TemplateExit(ctx, TemplateKey{WorkspaceID: ws, TemplateID: templateID})
	if err != nil || exit.Words == nil || len(exit.Words) != 0 {
		t.Fatalf("exit = %+v, %v", exit, err)
	}
}

func TestCampaignRoutesNeverTouchExits(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]buttonaction.Input{
		"exit kind":           {{ButtonIndex: 0, ButtonText: "Falar com atendente", Kind: buttonaction.KindOptOut}},
		"button with an exit": {{ButtonIndex: 1, ButtonText: "SAIR", Kind: buttonaction.KindHandOff, Params: buttonaction.Params{DepartmentID: "vendas"}}},
	}
	for name, inputs := range cases {
		if _, err := f.svc.SaveCampaignRoutes(ctx, actor(), campaignID, 1, inputs); !errors.Is(err, buttonaction.ErrExitLocked) {
			t.Errorf("%s: got %v, want ErrExitLocked", name, err)
		}
	}
}

func TestCampaignRouteChangesWhereANonExitButtonLeads(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.SaveCampaignRoutes(ctx, actor(), campaignID, 1, []buttonaction.Input{{ButtonIndex: 0, ButtonText: "Falar com atendente", Kind: buttonaction.KindHandOff, Params: buttonaction.Params{DepartmentID: "cobranca"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.Routes.Version != 2 || view.Buttons[0].Source != buttonaction.SourceCampaign {
		t.Fatalf("view = %+v", view)
	}
	got, err := f.svc.ResolveAction(ctx, ResolveInput{TemplateKey: TemplateKey{WorkspaceID: ws, TemplateID: templateID}, CampaignID: campaignID, ButtonIndex: 0})
	if err != nil || got.Action == nil || got.Action.Params.DepartmentID != "cobranca" || got.Source != buttonaction.SourceCampaign {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := f.svc.SaveCampaignRoutes(ctx, actor(), "other", 1, nil); !errors.Is(err, buttonaction.ErrCampaignNotFound) {
		t.Fatalf("other campaign: %v", err)
	}
}

func TestExitVerdictCountsTheWordsTheTemplateShows(t *testing.T) {
	f := newFixture(t)
	view, err := f.svc.ExitVerdict(ctx, actor(), templateID, "")
	if err != nil || view.Verdict.State != buttonaction.VerdictNone {
		t.Fatalf("a SAIR button without an action is no exit: %+v, %v", view, err)
	}
	f.templates.byID[templateID].Components = append(f.templates.byID[templateID].Components, template.TemplateComponent{Type: "FOOTER", Text: "Responda /sair para não receber"})
	view, err = f.svc.ExitVerdict(ctx, actor(), templateID, campaignID)
	if err != nil || view.Verdict.State != buttonaction.VerdictOK || len(view.Verdict.ExitWords) != 1 || view.Verdict.ExitWords[0] != "sair" {
		t.Fatalf("footer word = %+v, %v", view, err)
	}
}

func marketingDraft() template.CreateTemplateInput {
	tmpl := promoTemplate()
	return template.CreateTemplateInput{BusinessPhoneID: "phone-1", Name: "nova_promo", Language: "pt_BR", Category: template.TemplateCategoryMarketing, Components: tmpl.Components}
}

func TestCreatingAMarketingTemplateWithoutAShownExitIsRefused(t *testing.T) {
	f := newFixture(t)
	cases := map[string]CreateTemplateRequest{
		"nothing":                    {Template: marketingDraft()},
		"a confirmation alone":       {Template: marketingDraft(), ExitConfirmation: "Pronto."},
		"a route action on a button": {Template: marketingDraft(), Actions: []buttonaction.Input{{ButtonIndex: 0, ButtonText: "Falar com atendente", Kind: buttonaction.KindHandOff, Params: buttonaction.Params{DepartmentID: "vendas"}}}},
	}
	for name, req := range cases {
		if _, err := f.svc.CreateTemplate(ctx, actor(), req); !errors.Is(err, buttonaction.ErrMarketingNeedsExit) {
			t.Errorf("%s: got %v, want ErrMarketingNeedsExit", name, err)
		}
	}
	if len(f.templates.created) != 0 || f.settings.saves != 0 {
		t.Fatalf("a refused create reached Meta or stored something: created %d, saves %d", len(f.templates.created), f.settings.saves)
	}
}

func TestCreatingAMarketingTemplateWithAnExitActionSavesItBeforeCreating(t *testing.T) {
	f := newFixture(t)
	req := CreateTemplateRequest{Template: marketingDraft(), Actions: []buttonaction.Input{{ButtonIndex: 1, ButtonText: "SAIR", Kind: buttonaction.KindOptOut, Confirmation: "Entendido."}}}
	if _, err := f.svc.CreateTemplate(ctx, actor(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stored, _ := f.settings.Get(ctx, ws, buttonaction.TemplateRef{Name: "nova_promo", Language: "pt_BR"})
	if len(stored.Actions) != 1 || stored.Actions[0].Kind != buttonaction.KindOptOut {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestCreatingAMarketingTemplateThatShowsAnExitWordIsAccepted(t *testing.T) {
	f := newFixture(t)
	draft := marketingDraft()
	draft.Components = append(draft.Components, template.TemplateComponent{Type: "FOOTER", Text: "Responda /sair para não receber"})
	if _, err := f.svc.CreateTemplate(ctx, actor(), CreateTemplateRequest{Template: draft, ExitConfirmation: "Pronto, você saiu."}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stored, _ := f.settings.Get(ctx, ws, buttonaction.TemplateRef{Name: "nova_promo", Language: "pt_BR"})
	if len(f.templates.created) != 1 || stored.ExitConfirmation != "Pronto, você saiu." {
		t.Fatalf("created %d, stored = %+v", len(f.templates.created), stored)
	}
}

func TestCreatingAUtilityTemplateNeedsNoExit(t *testing.T) {
	f := newFixture(t)
	draft := marketingDraft()
	draft.Category = template.TemplateCategoryUtility
	if _, err := f.svc.CreateTemplate(ctx, actor(), CreateTemplateRequest{Template: draft}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.settings.saves != 0 {
		t.Fatalf("a template with no exit setup stores nothing, saves = %d", f.settings.saves)
	}
}

func TestAdmitCampaignRefusesATemplateWithoutExitUnlessAcknowledged(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.AdmitCampaign(ctx, ws, promoTemplate(), ""); !errors.Is(err, buttonaction.ErrExitRiskNotAcknowledged) {
		t.Fatalf("got %v, want ErrExitRiskNotAcknowledged", err)
	}
	admission, err := f.svc.AdmitCampaign(ctx, ws, promoTemplate(), userID)
	if err != nil {
		t.Fatalf("acknowledged: %v", err)
	}
	if len(f.routes.rows) != 0 {
		t.Fatalf("admitting writes nothing before the campaign is saved: %+v", f.routes.rows)
	}
	if err := f.svc.RecordCampaignAdmission(ctx, ws, campaignID, admission); err != nil {
		t.Fatalf("record: %v", err)
	}
	routes, _ := f.routes.Get(ctx, ws, campaignID)
	if routes.ExitRisk == nil || routes.ExitRisk.By != userID || routes.Template.Name != "promo_outubro" {
		t.Fatalf("routes = %+v", routes)
	}
}

func TestAdmitCampaignOfATemplateWithAnExitRecordsNothing(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); err != nil {
		t.Fatal(err)
	}
	admission, err := f.svc.AdmitCampaign(ctx, ws, promoTemplate(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := f.svc.RecordCampaignAdmission(ctx, ws, campaignID, admission); err != nil {
		t.Fatalf("record: %v", err)
	}
	if len(f.routes.rows) != 0 {
		t.Fatalf("nothing to acknowledge, rows = %+v", f.routes.rows)
	}
}
