package buttonaction_usecase

import (
	"context"

	"vozko/domain/buttonaction"
	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
)

type CampaignView struct {
	CampaignID string
	TemplateID string
	Template   *template.Template
	Routes     buttonaction.CampaignRoutes
	Buttons    []buttonaction.ButtonRoute
}

func (s *Service) CampaignRoutes(ctx context.Context, a Actor, campaignID string) (*CampaignView, error) {
	if err := requireActor(a); err != nil {
		return nil, err
	}
	tmpl, err := s.campaignTemplate(a, campaignID)
	if err != nil {
		return nil, err
	}
	settings, err := s.deps.Settings.Get(ctx, a.WorkspaceID, buttonaction.RefOf(tmpl))
	if err != nil {
		return nil, err
	}
	routes, err := s.deps.Routes.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return nil, err
	}
	return campaignViewOf(campaignID, tmpl, settings, routes), nil
}

func (s *Service) SaveCampaignRoutes(ctx context.Context, a Actor, campaignID string, expectedVersion int64, inputs []buttonaction.Input) (*CampaignView, error) {
	if err := requireActor(a); err != nil {
		return nil, err
	}
	if err := shared.RequireVersion(expectedVersion); err != nil {
		return nil, err
	}
	tmpl, err := s.campaignTemplate(a, campaignID)
	if err != nil {
		return nil, err
	}
	settings, err := s.deps.Settings.Get(ctx, a.WorkspaceID, buttonaction.RefOf(tmpl))
	if err != nil {
		return nil, err
	}
	current, err := s.deps.Routes.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return nil, err
	}
	if current.Version != expectedVersion {
		return nil, buttonaction.ErrVersionConflict
	}
	routes, err := s.buildActions(ctx, a, tmpl, inputs, current.Routes)
	if err != nil {
		return nil, err
	}
	for _, route := range routes {
		if err := buttonaction.ValidateOverride(tmpl, settings, route); err != nil {
			return nil, err
		}
	}
	next := buttonaction.CampaignRoutes{
		WorkspaceID: a.WorkspaceID, CampaignID: campaignID, Template: buttonaction.RefOf(tmpl), Routes: routes, UpdatedBy: a.UserID,
	}
	if err := s.deps.Routes.Save(ctx, &next, expectedVersion); err != nil {
		return nil, err
	}
	if err := s.forget(campaignScope(a.WorkspaceID, campaignID)); err != nil {
		return nil, err
	}
	next.ExitRisk = current.ExitRisk
	return campaignViewOf(campaignID, tmpl, settings, next), nil
}

func (s *Service) campaignTemplate(a Actor, campaignID string) (*template.Template, error) {
	c, err := s.campaign(a, campaignID)
	if err != nil {
		return nil, err
	}
	return s.template(a.WorkspaceID, c.TemplateID)
}

func campaignViewOf(campaignID string, tmpl *template.Template, settings buttonaction.TemplateSettings, routes buttonaction.CampaignRoutes) *CampaignView {
	return &CampaignView{
		CampaignID: campaignID,
		TemplateID: tmpl.ID,
		Template:   tmpl,
		Routes:     routes.Normalized(),
		Buttons:    buttonaction.RoutesOf(tmpl, settings, routes),
	}
}
