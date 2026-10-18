package buttonaction_usecase

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
)

type CreateTemplateRequest struct {
	Template         template.CreateTemplateInput
	Actions          []buttonaction.Input
	ExitConfirmation string
}

func (s *Service) CreateTemplate(ctx context.Context, a Actor, req CreateTemplateRequest) (*template.CreateTemplateOutput, error) {
	if err := requireActor(a); err != nil {
		return nil, err
	}
	probe := probeTemplate(req.Template)
	ref := buttonaction.RefOf(probe)
	if ref.Empty() {
		return nil, template.ErrTemplateNameRequired
	}
	current, err := s.deps.Settings.Get(ctx, a.WorkspaceID, ref)
	if err != nil {
		return nil, err
	}
	change := TemplateChange{Actions: req.Actions, ExitConfirmation: req.ExitConfirmation}
	next, err := s.settingsForCreate(ctx, a, probe, current, change)
	if err != nil {
		return nil, err
	}
	verdict := buttonaction.ExitVerdict(probe, next, buttonaction.CampaignRoutes{})
	if err := buttonaction.RequireTemplateExit(probe.Category, verdict); err != nil {
		return nil, err
	}
	created, err := s.deps.Templates.Create(a.WorkspaceID, a.UserID, req.Template)
	if err != nil {
		return nil, err
	}
	if !change.carriesExitSetup() {
		return created, nil
	}
	if err := s.deps.Settings.Save(ctx, &next, current.Version); err != nil {
		return nil, fmt.Errorf("the template was created, but its exit setup was not saved: %w", err)
	}
	if err := s.forget(templateScope(a.WorkspaceID, ref)); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Service) AdmitCampaign(ctx context.Context, workspaceID string, tmpl *template.Template, acknowledgedBy string) (buttonaction.CampaignAdmission, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return buttonaction.CampaignAdmission{}, buttonaction.ErrWorkspaceRequired
	}
	if tmpl == nil {
		return buttonaction.CampaignAdmission{}, buttonaction.ErrTemplateNotFound
	}
	verdict, err := s.verdictOf(ctx, workspaceID, tmpl, buttonaction.CampaignRoutes{})
	if err != nil {
		return buttonaction.CampaignAdmission{}, err
	}
	ack := s.acknowledgement(verdict, strings.TrimSpace(acknowledgedBy))
	if err := buttonaction.RequireExitRiskAcknowledged(verdict, ack); err != nil {
		return buttonaction.CampaignAdmission{}, err
	}
	return buttonaction.CampaignAdmission{Template: buttonaction.RefOf(tmpl), ExitRisk: ack}, nil
}

func (s *Service) RecordCampaignAdmission(ctx context.Context, workspaceID, campaignID string, admission buttonaction.CampaignAdmission) error {
	current, err := s.deps.Routes.Get(ctx, workspaceID, campaignID)
	if err != nil {
		return err
	}
	next, write := current.Admit(admission)
	if !write {
		return nil
	}
	if err := s.deps.Routes.Save(ctx, &next, current.Version); err != nil {
		return err
	}
	return s.forget(campaignScope(workspaceID, campaignID))
}

func (s *Service) acknowledgement(verdict buttonaction.Verdict, by string) *buttonaction.Acknowledgement {
	if verdict.State == buttonaction.VerdictOK || by == "" {
		return nil
	}
	return &buttonaction.Acknowledgement{By: by, At: s.now()}
}

func (s *Service) settingsForCreate(ctx context.Context, a Actor, probe *template.Template, current buttonaction.TemplateSettings, change TemplateChange) (buttonaction.TemplateSettings, error) {
	if !change.carriesExitSetup() {
		kept := current
		kept.UpdatedBy = a.UserID
		return kept, nil
	}
	return s.nextSettings(ctx, a, probe, current, change)
}

func (c TemplateChange) carriesExitSetup() bool {
	return len(c.Actions) > 0 || strings.TrimSpace(c.ExitConfirmation) != ""
}

func probeTemplate(in template.CreateTemplateInput) *template.Template {
	return &template.Template{
		Name:       in.Name,
		Language:   languageOf(in.Language),
		Category:   in.Category,
		Components: in.Components,
	}
}

func languageOf(language string) string {
	if strings.TrimSpace(language) == "" {
		return "pt_BR"
	}
	return language
}
