package inbox_assignment_usecase

import (
	"context"
	"errors"
	"fmt"

	"vozko/domain/shared"
)

// ErrAutomationForbidden: the caller cannot open this conversation, so they
// cannot switch its automation either.
var ErrAutomationForbidden = errors.New("inbox assignment: no access to this conversation")

// EntryAccess answers whether a caller may open a conversation.
type EntryAccess interface {
	CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool
}

type OperatorAutomationInput struct {
	ActorUserID string
	WorkspaceID string
	IsAdmin     bool
	EntryID     string
	EntryType   shared.EntryType
	// Enabled is the new switch state; nil clears the override so the
	// conversation follows its channel again, which is on.
	Enabled *bool
}

type OperatorAutomationResult struct {
	// Owner holds the conversation afterwards: a user id, ai:<id>,
	// workflow:<id>, or "" for the team queue.
	Owner string
}

// OperatorAutomationToggle is the automation switch as people use it. It is
// the existing per-conversation switch plus the ownership that must follow:
// pausing lets go of what the paused agent or workflow held, so it returns to
// the team instead of staying hidden; resuming hands the conversation back to
// the automation that governs it, so a person and the AI never answer the same
// contact. Hand-offs pause through the plain switch instead, since they have
// just moved the conversation to a person.
type OperatorAutomationToggle struct {
	automation AutomationPauser
	ownership  *AssignmentService
	access     EntryAccess
}

func NewOperatorAutomationToggle(automation AutomationPauser, ownership *AssignmentService, access EntryAccess) *OperatorAutomationToggle {
	return &OperatorAutomationToggle{automation: automation, ownership: ownership, access: access}
}

func (t *OperatorAutomationToggle) SetAutomation(ctx context.Context, in OperatorAutomationInput) (OperatorAutomationResult, error) {
	entryType := string(in.EntryType)
	if t.access == nil || !t.access.CanAccessEntry(in.ActorUserID, in.WorkspaceID, in.EntryID, entryType, in.IsAdmin) {
		return OperatorAutomationResult{}, ErrAutomationForbidden
	}

	if err := t.automation.SetAutomation(ctx, in.EntryID, in.EntryType, in.Enabled); err != nil {
		return OperatorAutomationResult{}, err
	}

	if in.Enabled != nil && !*in.Enabled {
		owner, err := t.ownership.TakeOverFromAutomation(in.EntryID, entryType, in.ActorUserID)
		if err != nil {
			return OperatorAutomationResult{}, fmt.Errorf("automation paused for %s (%s) but it still holds the conversation: %w", in.EntryID, in.EntryType, err)
		}
		// A paused AI did not resolve the conversation; whoever holds it now does.
		t.ownership.endAISession(in.WorkspaceID, in.EntryID, entryType, owner, sessionEndPaused)
		return OperatorAutomationResult{Owner: owner}, nil
	}

	owner, err := t.ownership.ReturnToAutomation(in.EntryID, entryType, in.ActorUserID)
	if err != nil && !errors.Is(err, ErrNothingToReturnTo) {
		return OperatorAutomationResult{}, fmt.Errorf("automation resumed for %s (%s) but the conversation was not handed back: %w", in.EntryID, in.EntryType, err)
	}
	return OperatorAutomationResult{Owner: owner}, nil
}
