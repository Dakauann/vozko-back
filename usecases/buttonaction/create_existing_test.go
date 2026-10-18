package buttonaction_usecase

import (
	"errors"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
)

var errMetaRefused = errors.New("meta: a template with this name already exists")

func duplicateOfPromo() template.CreateTemplateInput {
	tmpl := promoTemplate()
	return template.CreateTemplateInput{BusinessPhoneID: "phone-1", Name: tmpl.Name, Language: tmpl.Language, Category: tmpl.Category, Components: tmpl.Components}
}

func promoRef() buttonaction.TemplateRef {
	return buttonaction.RefOf(promoTemplate())
}

func TestADuplicateCreateRefusedByMetaKeepsTheStoredExit(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); err != nil {
		t.Fatal(err)
	}
	f.templates.createErr = errMetaRefused
	req := CreateTemplateRequest{
		Template:         duplicateOfPromo(),
		Actions:          []buttonaction.Input{{ButtonIndex: 1, ButtonText: "SAIR", Kind: buttonaction.KindOptOut, Confirmation: "Outra."}},
		ExitConfirmation: "Mudou.",
	}
	if _, err := f.svc.CreateTemplate(ctx, actor(), req); !errors.Is(err, errMetaRefused) {
		t.Fatalf("got %v, want the Meta refusal", err)
	}
	stored, _ := f.settings.Get(ctx, ws, promoRef())
	if len(stored.Actions) != 2 || stored.Actions[1].Confirmation != "Entendido." || stored.ExitConfirmation != "Pronto." {
		t.Fatalf("the stored exit changed: %+v", stored)
	}
}

func TestACreateRefusedByMetaStoresNothing(t *testing.T) {
	f := newFixture(t)
	f.templates.createErr = errMetaRefused
	req := CreateTemplateRequest{Template: marketingDraft(), Actions: []buttonaction.Input{{ButtonIndex: 1, ButtonText: "SAIR", Kind: buttonaction.KindOptOut}}}
	if _, err := f.svc.CreateTemplate(ctx, actor(), req); !errors.Is(err, errMetaRefused) {
		t.Fatalf("got %v, want the Meta refusal", err)
	}
	if f.settings.saves != 0 {
		t.Fatalf("a refused create stored its exit setup, saves = %d", f.settings.saves)
	}
}

func TestRecreatingATemplateNeedsTheCapabilityOnlyForWhatChanges(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); err != nil {
		t.Fatal(err)
	}
	delete(f.permission.granted, "leads:assign")
	change := optOutAndHandOff()
	req := CreateTemplateRequest{Template: duplicateOfPromo(), Actions: change.Actions, ExitConfirmation: change.ExitConfirmation}
	if _, err := f.svc.CreateTemplate(ctx, actor(), req); err != nil {
		t.Fatalf("keeping the stored hand off: %v", err)
	}
	stored, _ := f.settings.Get(ctx, ws, promoRef())
	if stored.Version != 3 || len(stored.Actions) != 2 {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestRecreatingATemplateCannotDropAnActionWithoutItsCapability(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); err != nil {
		t.Fatal(err)
	}
	delete(f.permission.granted, "leads:assign")
	req := CreateTemplateRequest{Template: duplicateOfPromo(), Actions: []buttonaction.Input{{ButtonIndex: 1, ButtonText: "SAIR", Kind: buttonaction.KindOptOut}}}
	if _, err := f.svc.CreateTemplate(ctx, actor(), req); !errors.Is(err, buttonaction.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
	if len(f.templates.created) != 0 {
		t.Fatal("a refused create reached Meta")
	}
}
