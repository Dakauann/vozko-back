package inbox_assignment_usecase

import (
	"context"
	"errors"
	"fmt"

	aa "vozko/domain/ai_attendance"
	"vozko/domain/shared"
)

var ErrAutomationForbidden = errors.New("inbox assignment: no access to this conversation")

type EntryAccess interface {
	CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool
}

type OperatorAutomationInput struct {
	ActorUserID string
	WorkspaceID string
	IsAdmin     bool
	EntryID     string
	EntryType   shared.EntryType
	Enabled     *bool
}

type OperatorAutomationResult struct {
	Owner string
}

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
		t.ownership.endAISession(in.WorkspaceID, in.EntryID, entryType, owner, in.ActorUserID, aa.EndReasonAutomationPaused)
		return OperatorAutomationResult{Owner: owner}, nil
	}

	owner, err := t.ownership.ReturnToAutomation(in.EntryID, entryType, in.ActorUserID)
	if err != nil {
		return OperatorAutomationResult{Owner: owner}, t.pauseAgain(ctx, in, err)
	}
	return OperatorAutomationResult{Owner: owner}, nil
}

func (t *OperatorAutomationToggle) pauseAgain(ctx context.Context, in OperatorAutomationInput, cause error) error {
	off := false
	if err := t.automation.SetAutomation(ctx, in.EntryID, in.EntryType, &off); err != nil {
		return fmt.Errorf("%w: %s (%s) could not be handed back (%v) nor paused again: %v", ErrAutomationStillActive, in.EntryID, in.EntryType, cause, err)
	}
	return fmt.Errorf("automation stays paused for %s (%s): %w", in.EntryID, in.EntryType, cause)
}
