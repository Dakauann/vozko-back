package buttonaction_usecase

import (
	"context"

	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
)

func (s *Service) verdictOf(ctx context.Context, workspaceID string, tmpl *template.Template, routes buttonaction.CampaignRoutes) (buttonaction.Verdict, error) {
	settings, err := s.deps.Settings.Get(ctx, workspaceID, buttonaction.RefOf(tmpl))
	if err != nil {
		return buttonaction.Verdict{}, err
	}
	return buttonaction.ExitVerdict(tmpl, settings, routes), nil
}

func (s *Service) ExitConfigured(ctx context.Context, workspaceID, templateID, campaignID string) (bool, error) {
	tmpl, err := s.template(workspaceID, templateID)
	if err != nil {
		return false, err
	}
	routes, err := s.deps.Routes.Get(ctx, workspaceID, campaignID)
	if err != nil {
		return false, err
	}
	verdict, err := s.verdictOf(ctx, workspaceID, tmpl, routes)
	if err != nil {
		return false, err
	}
	return verdict.State == buttonaction.VerdictOK, nil
}
