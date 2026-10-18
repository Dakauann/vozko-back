package buttonaction_usecase

import (
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
)

func TestTheVerdictSaysWhichGatesNeedTheAcknowledgement(t *testing.T) {
	f := newFixture(t)
	view, err := f.svc.ExitVerdict(ctx, actor(), templateID, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Verdict.State != buttonaction.VerdictNone || view.Gates != (buttonaction.ExitGates{TemplateExitRequired: true, CampaignAcknowledgementRequired: true}) {
		t.Fatalf("view = %+v", view)
	}

	f.templates.byID[templateID].Category = template.TemplateCategoryUtility
	view, err = f.svc.ExitVerdict(ctx, actor(), templateID, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Gates != (buttonaction.ExitGates{CampaignAcknowledgementRequired: true}) {
		t.Fatalf("a utility template still gates its campaigns: %+v", view.Gates)
	}

	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); err != nil {
		t.Fatal(err)
	}
	view, err = f.svc.ExitVerdict(ctx, actor(), templateID, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Gates != (buttonaction.ExitGates{}) {
		t.Fatalf("a template with an exit needs none: %+v", view.Gates)
	}
}

func TestTheDraftVerdictSaysWhichGatesNeedTheAcknowledgement(t *testing.T) {
	f := newFixture(t)
	view, err := f.svc.DraftExitVerdict(ctx, actor(), DraftTemplate{Category: template.TemplateCategoryUtility, Buttons: []template.TemplateButton{{Type: "QUICK_REPLY", Text: "Ok"}}})
	if err != nil {
		t.Fatal(err)
	}
	if view.Gates != (buttonaction.ExitGates{CampaignAcknowledgementRequired: true}) {
		t.Fatalf("view = %+v", view.Gates)
	}
	if len(view.AllowedKinds) != 3 || view.AllowedKinds[0] != buttonaction.KindOptOut || view.AllowedKinds[2] != buttonaction.KindClassify {
		t.Fatalf("the draft lists %v", view.AllowedKinds)
	}
}

func TestTheActionsViewListsTheKindsThisViewerMaySet(t *testing.T) {
	f := newFixture(t)
	view, err := f.svc.TemplateActions(ctx, actor(), templateID)
	if err != nil {
		t.Fatal(err)
	}
	want := []buttonaction.Kind{buttonaction.KindOptOut, buttonaction.KindHandOff, buttonaction.KindClassify}
	if len(view.AllowedKinds) != len(want) {
		t.Fatalf("allowed = %v, want %v", view.AllowedKinds, want)
	}
	for i, kind := range want {
		if view.AllowedKinds[i] != kind {
			t.Fatalf("allowed = %v, want %v", view.AllowedKinds, want)
		}
	}

	saved, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff())
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.AllowedKinds) != len(want) {
		t.Fatalf("the saved view lists %v", saved.AllowedKinds)
	}
}
