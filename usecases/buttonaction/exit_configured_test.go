package buttonaction_usecase

import (
	"context"
	"errors"
	"testing"
	"vozko/domain/buttonaction"
)

type draftRoutesGuard struct{ calls int }

func (s *draftRoutesGuard) Get(context.Context, string, string) (buttonaction.CampaignRoutes, error) {
	s.calls++
	return buttonaction.CampaignRoutes{}, buttonaction.ErrCampaignNotFound
}
func (*draftRoutesGuard) Save(context.Context, *buttonaction.CampaignRoutes, int64) error {
	return errors.New("unexpected save")
}

func TestDraftExitValidationDoesNotReadRoutesForACampaignThatDoesNotExistYet(t *testing.T) {
	f := newFixture(t)
	routes := &draftRoutesGuard{}
	f.svc.deps.Routes = routes
	f.templates.byID[templateID].Components[0].Text = "Para cancelar, digite /sair."
	configured, err := f.svc.ExitConfigured(context.Background(), ws, templateID, "")
	if err != nil || !configured || routes.calls != 0 {
		t.Fatalf("draft configured=%v err=%v route reads=%d", configured, err, routes.calls)
	}
	if _, err := f.svc.ExitConfigured(context.Background(), ws, templateID, "missing"); !errors.Is(err, buttonaction.ErrCampaignNotFound) {
		t.Fatalf("stored campaign error swallowed: %v", err)
	}
}

func TestDraftWithoutAnExitIsReportedAsUnconfigured(t *testing.T) {
	f := newFixture(t)
	f.svc.deps.Routes = &draftRoutesGuard{}
	configured, err := f.svc.ExitConfigured(context.Background(), ws, templateID, "")
	if err != nil || configured {
		t.Fatalf("draft configured=%v err=%v", configured, err)
	}
}

func TestACampaignWithoutAnyExitIsNotConfigured(t *testing.T) {
	f := newFixture(t)

	configured, err := f.svc.ExitConfigured(context.Background(), ws, templateID, campaignID)

	if err != nil || configured {
		t.Fatalf("configured = %v, err = %v", configured, err)
	}
}

func TestAnExitWordTheTemplateShowsCountsAsAnExit(t *testing.T) {
	f := newFixture(t)
	f.templates.byID[templateID].Components[0].Text = "Ofertas da semana. Para cancelar, digite /sair."

	configured, err := f.svc.ExitConfigured(context.Background(), ws, templateID, campaignID)

	if err != nil || !configured {
		t.Fatalf("configured = %v, err = %v", configured, err)
	}
}

func TestAWorkspaceWithoutTheTemplateGetsAnErrorNotAPass(t *testing.T) {
	f := newFixture(t)

	if _, err := f.svc.ExitConfigured(context.Background(), "ws-stranger", templateID, campaignID); err == nil {
		t.Fatal("a workspace without the template must not be told its exit is configured")
	}
}
