package buttonaction_usecase

import (
	"context"
	"fmt"

	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
)

type EditTemplateRequest struct {
	TemplateID       string
	Edit             template.Edit
	Actions          []buttonaction.Input
	ExitConfirmation string
}

func (s *Service) SetContentEditor(editor template.ContentEditor) {
	s.editor = editor
}

func (s *Service) EditTemplate(ctx context.Context, a Actor, req EditTemplateRequest) (*template.Template, error) {
	if err := requireActor(a); err != nil {
		return nil, err
	}
	if s.editor == nil {
		return nil, buttonaction.ErrUnavailable
	}
	stored, err := s.template(a.WorkspaceID, req.TemplateID)
	if err != nil {
		return nil, err
	}
	probe := *stored
	probe.Category, probe.Components = req.Edit.CategoryFor(stored), req.Edit.Components
	ref := buttonaction.RefOf(&probe)
	current, err := s.deps.Settings.Get(ctx, a.WorkspaceID, ref)
	if err != nil {
		return nil, err
	}
	change := TemplateChange{Actions: req.Actions, ExitConfirmation: req.ExitConfirmation}
	next, err := s.settingsForCreate(ctx, a, &probe, current, change)
	if err != nil {
		return nil, err
	}
	if err := buttonaction.RequireTemplateExit(probe.Category, buttonaction.ExitVerdict(&probe, next, buttonaction.CampaignRoutes{})); err != nil {
		return nil, err
	}
	edited, err := s.editor.EditContent(ctx, template.EditContentInput{WorkspaceID: a.WorkspaceID, UserID: a.UserID, TemplateID: req.TemplateID, Edit: req.Edit})
	if err != nil {
		return nil, err
	}
	if !change.carriesExitSetup() {
		return edited, nil
	}
	if err := s.deps.Settings.Save(ctx, &next, current.Version); err != nil {
		return nil, fmt.Errorf("the template was edited on Meta, but its exit setup was not saved: %w", err)
	}
	if err := s.forget(templateScope(a.WorkspaceID, ref)); err != nil {
		return nil, err
	}
	return edited, nil
}
