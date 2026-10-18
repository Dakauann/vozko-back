package buttonaction_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/buttonaction"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestTemplateSettingsRoundTripOnPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "buttonactions", &schema.TemplateButtonAction{}, &schema.TemplateExitSettings{})
	store := NewTemplateSettingsStore(db)
	ctx := context.Background()
	ref := buttonaction.TemplateRef{Name: "promo_outubro", Language: "pt_BR"}

	empty, err := store.Get(ctx, workspaceA, ref)
	if err != nil || empty.Version != buttonaction.FirstVersion {
		t.Fatalf("empty = %+v, %v", empty, err)
	}

	settings := &buttonaction.TemplateSettings{
		WorkspaceID: workspaceA, Template: ref, UpdatedBy: "user-1", ExitConfirmation: "Pronto.",
		Actions: []buttonaction.ButtonAction{
			{Template: ref, ButtonIndex: 0, ButtonText: "Falar com atendente", Kind: buttonaction.KindHandOff, Params: buttonaction.Params{DepartmentID: "dep-1"}},
			{Template: ref, ButtonIndex: 1, ButtonText: "SAIR", Kind: buttonaction.KindOptOut, Confirmation: "Entendido."},
		},
	}
	if err := store.Save(ctx, settings, buttonaction.FirstVersion); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if settings.Version != 2 {
		t.Fatalf("version = %d, want 2", settings.Version)
	}

	got, err := store.Get(ctx, workspaceA, ref)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || len(got.Actions) != 2 || got.Actions[1].Kind != buttonaction.KindOptOut || got.Actions[1].Confirmation != "Entendido." ||
		got.Actions[0].Params.DepartmentID != "dep-1" || got.ExitConfirmation != "Pronto." {
		t.Fatalf("got %+v", got)
	}

	if err := store.Save(ctx, &buttonaction.TemplateSettings{WorkspaceID: workspaceA, Template: ref}, buttonaction.FirstVersion); !errors.Is(err, buttonaction.ErrVersionConflict) {
		t.Fatalf("second first save: %v, want conflict", err)
	}

	trimmed := &buttonaction.TemplateSettings{WorkspaceID: workspaceA, Template: ref, UpdatedBy: "user-2", Actions: got.Actions[1:]}
	if err := store.Save(ctx, trimmed, 2); err != nil {
		t.Fatalf("second save: %v", err)
	}
	again, err := store.Get(ctx, workspaceA, ref)
	if err != nil {
		t.Fatal(err)
	}
	if again.Version != 3 || len(again.Actions) != 1 || again.Actions[0].ButtonIndex != 1 || again.ExitConfirmation != "" {
		t.Fatalf("again = %+v", again)
	}
	if err := store.Save(ctx, trimmed, 2); !errors.Is(err, buttonaction.ErrVersionConflict) {
		t.Fatalf("stale save: %v, want conflict", err)
	}
}

func TestCampaignRoutesRoundTripOnPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "buttonroutes", &schema.WhatsAppCampaignButtonRoutes{})
	store := NewCampaignRoutesStore(db)
	ctx := context.Background()
	ref := buttonaction.TemplateRef{Name: "promo_outubro", Language: "pt_BR"}

	routes := &buttonaction.CampaignRoutes{
		WorkspaceID: workspaceA, CampaignID: campaignA, Template: ref, UpdatedBy: "user-1",
		ExitRisk: &buttonaction.Acknowledgement{By: "user-1", At: time.Now().UTC()},
		Routes:   []buttonaction.ButtonAction{{Template: ref, ButtonIndex: 0, ButtonText: "Falar com atendente", Kind: buttonaction.KindHandOff, Params: buttonaction.Params{DepartmentID: "cobranca"}}},
	}
	if err := store.Save(ctx, routes, buttonaction.FirstVersion); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := store.Get(ctx, workspaceA, campaignA)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || got.Template != ref || len(got.Routes) != 1 || got.Routes[0].Params.DepartmentID != "cobranca" || got.ExitRisk == nil {
		t.Fatalf("got %+v", got)
	}
	other, err := store.Get(ctx, "7a1d1c52-2a6c-4b8e-9f43-1c1f5b7d2a02", campaignA)
	if err != nil || len(other.Routes) != 0 || other.Version != buttonaction.FirstVersion {
		t.Fatalf("another workspace must not see the routes: %+v, %v", other, err)
	}
	if err := store.Save(ctx, &buttonaction.CampaignRoutes{WorkspaceID: workspaceA, CampaignID: campaignA, Template: ref}, 2); err != nil {
		t.Fatalf("second save: %v", err)
	}
	cleared, _ := store.Get(ctx, workspaceA, campaignA)
	if cleared.Version != 3 || len(cleared.Routes) != 0 || cleared.ExitRisk == nil {
		t.Fatalf("cleared = %+v", cleared)
	}
}
