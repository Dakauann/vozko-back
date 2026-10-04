package inbox_assignment_usecase

import (
	"context"
	"fmt"

	"vozko/domain/agent"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type AgentDirectory interface {
	FindByID(agentID string) (*agent.Agent, error)
}

type WorkflowDirectory interface {
	FindByID(workflowID string) (*workflow.Workflow, error)
}

type AutomationDirectory struct {
	Agents    AgentDirectory
	Workflows WorkflowDirectory
}

func (d AutomationDirectory) usable(workspaceID string, a conversation.Automation) error {
	if !a.Valid() {
		return conversation.ErrAutomationInvalid
	}
	switch a.Kind {
	case conversation.AutomationAgent:
		found, err := d.Agents.FindByID(a.ID)
		if err == nil && found != nil && found.WorkspaceID == workspaceID && found.IsActive {
			return nil
		}
	case conversation.AutomationWorkflow:
		found, err := d.Workflows.FindByID(a.ID)
		if err == nil && found != nil && found.WorkspaceID == workspaceID && found.Status == workflow.WorkflowStatusActive && found.Type != workflow.WorkflowTypeVoice {
			return nil
		}
	}
	return conversation.ErrAutomationUnusable
}

type DelegateInput struct {
	ActorUserID string
	WorkspaceID string
	IsAdmin     bool
	EntryID     string
	EntryType   shared.EntryType
	Automation  conversation.Automation
}

func (in DelegateInput) toggle() OperatorAutomationInput {
	on := true
	return OperatorAutomationInput{
		ActorUserID: in.ActorUserID,
		WorkspaceID: in.WorkspaceID,
		IsAdmin:     in.IsAdmin,
		EntryID:     in.EntryID,
		EntryType:   in.EntryType,
		Enabled:     &on,
	}
}

type AutomationDelegation struct {
	delegations conversation.DelegationRepository
	toggle      *OperatorAutomationToggle
	directory   AutomationDirectory
}

func NewAutomationDelegation(delegations conversation.DelegationRepository, toggle *OperatorAutomationToggle, directory AutomationDirectory) *AutomationDelegation {
	return &AutomationDelegation{delegations: delegations, toggle: toggle, directory: directory}
}

func (d *AutomationDelegation) Delegate(ctx context.Context, in DelegateInput) (OperatorAutomationResult, error) {
	if !d.toggle.allows(in.toggle()) {
		return OperatorAutomationResult{}, ErrAutomationForbidden
	}
	if err := d.directory.usable(in.WorkspaceID, in.Automation); err != nil {
		return OperatorAutomationResult{}, err
	}
	previous, err := d.delegations.Find(ctx, in.EntryID, in.EntryType)
	if err != nil {
		return OperatorAutomationResult{}, fmt.Errorf("delegate %s (%s): %w", in.EntryID, in.EntryType, err)
	}
	if err := d.delegations.Save(ctx, conversation.Delegation{
		WorkspaceID: in.WorkspaceID,
		EntryID:     in.EntryID,
		EntryType:   in.EntryType,
		Automation:  in.Automation,
		DelegatedBy: in.ActorUserID,
	}); err != nil {
		return OperatorAutomationResult{}, fmt.Errorf("delegate %s (%s): %w", in.EntryID, in.EntryType, err)
	}

	result, err := d.toggle.SetAutomation(ctx, in.toggle())
	if err != nil {
		if restoreErr := d.restore(ctx, in, previous); restoreErr != nil {
			return result, fmt.Errorf("%w; the previous delegation could not be restored: %v", err, restoreErr)
		}
		return result, err
	}
	return result, nil
}

func (d *AutomationDelegation) restore(ctx context.Context, in DelegateInput, previous *conversation.Delegation) error {
	if previous == nil {
		return d.delegations.Delete(ctx, in.EntryID, in.EntryType)
	}
	return d.delegations.Save(ctx, *previous)
}
