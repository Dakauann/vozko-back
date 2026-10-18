package buttonaction_usecase

import (
	"context"

	"vozko/domain/buttonaction"
	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
)

type TemplateView struct {
	TemplateID   string
	Template     *template.Template
	Settings     buttonaction.TemplateSettings
	Buttons      []buttonaction.ButtonRoute
	AllowedKinds []buttonaction.Kind
}

type TemplateChange struct {
	Actions          []buttonaction.Input
	ExitConfirmation string
}

func (s *Service) TemplateActions(ctx context.Context, a Actor, templateID string) (*TemplateView, error) {
	if err := requireActor(a); err != nil {
		return nil, err
	}
	tmpl, err := s.template(a.WorkspaceID, templateID)
	if err != nil {
		return nil, err
	}
	settings, err := s.deps.Settings.Get(ctx, a.WorkspaceID, buttonaction.RefOf(tmpl))
	if err != nil {
		return nil, err
	}
	return s.viewFor(a, tmpl, settings), nil
}

func (s *Service) SaveTemplateActions(ctx context.Context, a Actor, templateID string, expectedVersion int64, change TemplateChange) (*TemplateView, error) {
	if err := requireActor(a); err != nil {
		return nil, err
	}
	if err := shared.RequireVersion(expectedVersion); err != nil {
		return nil, err
	}
	tmpl, err := s.template(a.WorkspaceID, templateID)
	if err != nil {
		return nil, err
	}
	ref := buttonaction.RefOf(tmpl)
	current, err := s.deps.Settings.Get(ctx, a.WorkspaceID, ref)
	if err != nil {
		return nil, err
	}
	if current.Version != expectedVersion {
		return nil, buttonaction.ErrVersionConflict
	}
	next, err := s.nextSettings(ctx, a, tmpl, current, change)
	if err != nil {
		return nil, err
	}
	if err := s.deps.Settings.Save(ctx, &next, expectedVersion); err != nil {
		return nil, err
	}
	if err := s.forget(templateScope(a.WorkspaceID, ref)); err != nil {
		return nil, err
	}
	return s.viewFor(a, tmpl, next), nil
}

func (s *Service) nextSettings(ctx context.Context, a Actor, tmpl *template.Template, current buttonaction.TemplateSettings, change TemplateChange) (buttonaction.TemplateSettings, error) {
	actions, err := s.buildActions(ctx, a, tmpl, change.Actions, current.Actions)
	if err != nil {
		return buttonaction.TemplateSettings{}, err
	}
	confirmation, err := buttonaction.NewExitConfirmation(change.ExitConfirmation)
	if err != nil {
		return buttonaction.TemplateSettings{}, err
	}
	return buttonaction.TemplateSettings{
		WorkspaceID:      a.WorkspaceID,
		Template:         buttonaction.RefOf(tmpl),
		Actions:          actions,
		ExitConfirmation: confirmation,
		UpdatedBy:        a.UserID,
	}, nil
}

func (s *Service) viewFor(a Actor, tmpl *template.Template, settings buttonaction.TemplateSettings) *TemplateView {
	return &TemplateView{
		TemplateID:   tmpl.ID,
		Template:     tmpl,
		Settings:     settings.Normalized(),
		Buttons:      buttonaction.RoutesOf(tmpl, settings, buttonaction.CampaignRoutes{}),
		AllowedKinds: s.allowedKinds(a),
	}
}

func (s *Service) allowedKinds(a Actor) []buttonaction.Kind {
	allowed := []buttonaction.Kind{}
	for _, kind := range buttonaction.Kinds() {
		if s.requireKind(a, kind) == nil {
			allowed = append(allowed, kind)
		}
	}
	return allowed
}

type VerdictView struct {
	Template *template.Template
	Verdict  buttonaction.Verdict
	Gates    buttonaction.ExitGates
}

func (s *Service) ExitVerdict(ctx context.Context, a Actor, templateID, campaignID string) (*VerdictView, error) {
	if err := requireActor(a); err != nil {
		return nil, err
	}
	tmpl, err := s.template(a.WorkspaceID, templateID)
	if err != nil {
		return nil, err
	}
	routes, err := s.campaignRoutesFor(ctx, a, campaignID)
	if err != nil {
		return nil, err
	}
	verdict, err := s.verdictOf(ctx, a.WorkspaceID, tmpl, routes)
	if err != nil {
		return nil, err
	}
	return &VerdictView{Template: tmpl, Verdict: verdict, Gates: buttonaction.ExitGatesOf(tmpl.Category, verdict)}, nil
}

func (s *Service) campaignRoutesFor(ctx context.Context, a Actor, campaignID string) (buttonaction.CampaignRoutes, error) {
	if campaignID == "" {
		return buttonaction.CampaignRoutes{}, nil
	}
	if _, err := s.campaign(a, campaignID); err != nil {
		return buttonaction.CampaignRoutes{}, err
	}
	return s.deps.Routes.Get(ctx, a.WorkspaceID, campaignID)
}
