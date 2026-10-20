package whatsappform_usecase

import (
	"context"

	wf "vozko/domain/whatsappform"
	"vozko/domain/workspace"
)

const (
	AutomationTrigger = "trigger"
	AutomationNode    = "node"

	CapabilityCreateWorkflows workspace.CapabilityKey = "workflows.create"
)

type FormAutomation struct {
	WorkflowID string `json:"workflowId"`
	Name       string `json:"name"`
	Active     bool   `json:"active"`
	Use        string `json:"use"`
}

type AutomationCatalog interface {
	List(ctx context.Context, workspaceID, formID string) ([]FormAutomation, error)
	Scaffold(ctx context.Context, workspaceID, userID, formID, formName string) (string, error)
}

func (s *Service) Automations(ctx context.Context, a Actor, formID string) ([]FormAutomation, error) {
	if err := s.require(a, wf.CapabilityView); err != nil {
		return nil, err
	}
	if _, err := s.deps.Forms.Get(ctx, a.WorkspaceID, formID); err != nil {
		return nil, err
	}
	listed, err := s.deps.Automations.List(ctx, a.WorkspaceID, formID)
	if err != nil {
		return nil, err
	}
	return append([]FormAutomation{}, listed...), nil
}

func (s *Service) ScaffoldAutomation(ctx context.Context, a Actor, formID string) (string, error) {
	if err := s.require(a, wf.CapabilityView, CapabilityCreateWorkflows); err != nil {
		return "", err
	}
	form, err := s.activeForm(ctx, a.WorkspaceID, formID)
	if err != nil {
		return "", err
	}
	return s.deps.Automations.Scaffold(ctx, a.WorkspaceID, a.UserID, form.ID, form.Name)
}
