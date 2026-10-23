package inbox_assignment_usecase

import (
	"context"
	"fmt"

	"vozko/domain/actor"
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

func (d AutomationDirectory) Usable(workspaceID string, a conversation.Automation) error {
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
		if err == nil && found.StartsConversationsIn(workspaceID) == nil {
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
	runs        ConversationAutomationRuns
}

type ConversationAutomationRuns interface {
	StopConversationRuns(ctx context.Context, workspaceID, entryID, entryType string) error
	StartConversationWorkflow(ctx context.Context, workspaceID, entryID, entryType, workflowID, by string) error
}

func NewAutomationDelegation(delegations conversation.DelegationRepository, toggle *OperatorAutomationToggle, directory AutomationDirectory, runs ConversationAutomationRuns) *AutomationDelegation {
	return &AutomationDelegation{delegations: delegations, toggle: toggle, directory: directory, runs: runs}
}

type SystemDelegation struct {
	WorkspaceID string
	EntryID     string
	EntryType   shared.EntryType
	Automation  conversation.Automation
}

func (d *AutomationDelegation) Delegate(ctx context.Context, in DelegateInput) (OperatorAutomationResult, error) {
	if !d.toggle.allows(in.toggle()) {
		return OperatorAutomationResult{}, ErrAutomationForbidden
	}
	return d.delegate(ctx, in)
}

func (d *AutomationDelegation) DelegateAsSystem(ctx context.Context, in SystemDelegation) error {
	_, err := d.delegate(ctx, DelegateInput{
		ActorUserID: actor.SystemID,
		WorkspaceID: in.WorkspaceID,
		EntryID:     in.EntryID,
		EntryType:   in.EntryType,
		Automation:  in.Automation,
	})
	return err
}

func (d *AutomationDelegation) delegate(ctx context.Context, in DelegateInput) (OperatorAutomationResult, error) {
	if err := d.directory.Usable(in.WorkspaceID, in.Automation); err != nil {
		return OperatorAutomationResult{}, err
	}
	previous, err := d.delegations.Find(ctx, in.EntryID, in.EntryType)
	if err != nil {
		return OperatorAutomationResult{}, fmt.Errorf("delegate %s (%s): %w", in.EntryID, in.EntryType, err)
	}
	if d.runs == nil {
		return OperatorAutomationResult{}, fmt.Errorf("delegate: conversation run switching is unavailable")
	}
	if err := d.runs.StopConversationRuns(ctx, in.WorkspaceID, in.EntryID, string(in.EntryType)); err != nil {
		return OperatorAutomationResult{}, err
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

	result, err := d.toggle.apply(ctx, in.toggle())
	if err != nil {
		if restoreErr := d.restore(ctx, in, previous); restoreErr != nil {
			return result, fmt.Errorf("%w; the previous delegation could not be restored: %v", err, restoreErr)
		}
		return result, err
	}
	if in.Automation.Kind == conversation.AutomationWorkflow {
		if err := d.runs.StartConversationWorkflow(ctx, in.WorkspaceID, in.EntryID, string(in.EntryType), in.Automation.ID, in.ActorUserID); err != nil {
			return result, d.toggle.pauseAgain(ctx, in.toggle(), fmt.Errorf("start the delegated workflow: %w", err))
		}
	}
	return result, nil
}

func (d *AutomationDelegation) restore(ctx context.Context, in DelegateInput, previous *conversation.Delegation) error {
	if previous == nil {
		return d.delegations.Delete(ctx, in.EntryID, in.EntryType)
	}
	return d.delegations.Save(ctx, *previous)
}
