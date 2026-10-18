package buttonaction_usecase

import (
	"errors"
	"testing"

	"vozko/domain/buttonaction"
)

func renameExitButton(f *fixture, text string) {
	f.templates.byID[templateID].Components[1].Buttons[1].Text = text
}

func handOffAndOptOutSeenAs(exitText string) TemplateChange {
	change := optOutAndHandOff()
	change.Actions[1].ButtonText = exitText
	return change
}

func TestSavingAgainKeepsAStaleActionStale(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); err != nil {
		t.Fatal(err)
	}
	renameExitButton(f, "Parar")

	view, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 2, handOffAndOptOutSeenAs("SAIR"))
	if err != nil {
		t.Fatalf("echoing the stale action: %v", err)
	}
	kept, _ := view.Settings.ActionAt(1)
	if kept.ButtonText != "SAIR" || !view.Buttons[1].Stale || view.Buttons[1].Action != nil {
		t.Fatalf("kept = %+v, button = %+v; want the action still stale", kept, view.Buttons[1])
	}
}

func TestReconfirmingAStaleActionWithTheShownTextBindsItAgain(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); err != nil {
		t.Fatal(err)
	}
	renameExitButton(f, "Parar")

	view, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 2, handOffAndOptOutSeenAs("Parar"))
	if err != nil {
		t.Fatalf("reconfirming: %v", err)
	}
	bound, _ := view.Settings.ActionAt(1)
	if bound.ButtonText != "Parar" || view.Buttons[1].Stale || view.Buttons[1].Action == nil {
		t.Fatalf("bound = %+v, button = %+v; want the action fresh again", bound, view.Buttons[1])
	}
}

func TestAnActionSavedAgainstATextTheTemplateNoLongerHasIsRefused(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); err != nil {
		t.Fatal(err)
	}
	renameExitButton(f, "Parar")
	saves := f.settings.saves

	changed := handOffAndOptOutSeenAs("SAIR")
	changed.Actions[1].Confirmation = "Outra confirmação."
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 2, changed); !errors.Is(err, buttonaction.ErrButtonActionStale) {
		t.Fatalf("changing a stale action under its old text = %v, want ErrButtonActionStale", err)
	}
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 2, handOffAndOptOutSeenAs("Quero mais")); !errors.Is(err, buttonaction.ErrButtonActionStale) {
		t.Fatalf("a text nobody saw = %v, want ErrButtonActionStale", err)
	}
	if f.settings.saves != saves {
		t.Fatalf("a refused save stored something")
	}
}

func TestACampaignRouteNeedsTheTextOfTheButtonItRoutes(t *testing.T) {
	f := newFixture(t)
	route := []buttonaction.Input{{ButtonIndex: 0, ButtonText: "Ver catálogo", Kind: buttonaction.KindHandOff, Params: buttonaction.Params{DepartmentID: "cobranca"}}}
	if _, err := f.svc.SaveCampaignRoutes(ctx, actor(), campaignID, 1, route); !errors.Is(err, buttonaction.ErrButtonActionStale) {
		t.Fatalf("got %v, want ErrButtonActionStale", err)
	}
}
