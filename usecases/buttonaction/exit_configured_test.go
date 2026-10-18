package buttonaction_usecase

import (
	"context"
	"testing"
)

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
