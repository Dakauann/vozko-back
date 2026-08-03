// Package crmbulk_usecase applies one CRM action to many conversation entries by
// fanning out to the EXISTING single-entry usecases (stage assign, label
// add/remove, inbox reassignment). It owns no mutation logic of its own: every
// target is routed to the same use case the single-entry HTTP handlers call, so
// bulk stays behaviourally identical to doing each action one at a time. The
// per-operation dependencies are declared here as narrow interfaces so the
// fan-out is unit-testable with mocks; the container wires the real usecases.
package crmbulk_usecase

import (
	"context"
	"errors"
	"fmt"

	"vozko/domain/conversation"
	"vozko/domain/label"
	"vozko/domain/stage"
	"vozko/domain/workspace"
)

// Ports (narrow, satisfied by the existing single-entry usecases) --------------

// StageAssigner moves an entry onto a stage.
// Satisfied by stage.AssignEntryStageUseCase (upsert: it replaces any current
// stage, so "move_stage" always succeeds).
type StageAssigner interface {
	Execute(workspaceID string, input stage.AssignEntryStageInput) (*stage.EntryStage, error)
}

// LabelAssigner adds a label to an entry.
// Satisfied by label.AssignEntryLabelUseCase.
type LabelAssigner interface {
	Execute(workspaceID string, input label.AssignEntryLabelInput) (*label.EntryLabel, error)
}

// LabelRemover removes a label from an entry.
// Satisfied by label.RemoveEntryLabelUseCase.
type LabelRemover interface {
	Execute(workspaceID, labelID, entryID, entryType string) error
}

// EntryAssigner reassigns an entry to a user.
// Satisfied by inbox_assignment_usecase.AssignmentService.
type EntryAssigner interface {
	Reassign(entryID, entryType, businessPhoneID, workspaceID, userID string) error
}

// Authorizer is the SAME gate the single-entry paths and the board already use,
// no bulk-specific auth logic. It enforces (a) per-action RBAC via
// HasWorkspacePermission and (b) per-entry scope via CanAccessEntry, which itself
// blocks BOTH cross-workspace and out-of-department entries. Satisfied by the
// existing conversation.Authorizer (narrow subset of ConversationAuthorizer).
type Authorizer interface {
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
	CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool
}

// Broadcaster pushes the same realtime updates the single-entry handlers emit, so
// a bulk change reaches every connected client's inbox/board. Narrow subset of the
// existing conversation.EventBroadcaster (satisfied by the conversation hub).
type Broadcaster interface {
	BroadcastStageUpdate(workspaceID, entryID, entryType string)
	BroadcastLabelUpdate(workspaceID, entryID, entryType string)
	BroadcastEntryUpdate(entryID, entryType string, message *conversation.Message)
}

// actionPermission maps each bulk action to the workspace permission its
// single-entry route already requires, so bulk enforces the SAME per-resource RBAC
// (reusing the domain/workspace constants, never hardcoded strings). ok=false is an
// unknown action.
func actionPermission(action string) (resource, act string, ok bool) {
	switch action {
	case ActionMoveStage:
		return string(workspace.ResourceStages), string(workspace.ActionAssign), true
	case ActionAssign:
		return string(workspace.ResourceConversations), string(workspace.ActionAssign), true
	case ActionAddLabel, ActionRemoveLabel:
		return string(workspace.ResourceLabels), string(workspace.ActionAssign), true
	default:
		return "", "", false
	}
}

// Actions ---------------------------------------------------------------------

const (
	ActionMoveStage   = "move_stage"   // Value = stageID
	ActionAssign      = "assign"       // Value = userID
	ActionAddLabel    = "add_label"    // Value = labelID
	ActionRemoveLabel = "remove_label" // Value = labelID
)

var (
	// ErrUnknownAction is returned per target when the requested action is not one
	// of the supported ActionX constants.
	ErrUnknownAction = errors.New("crmbulk: unknown action")
	// ErrForbiddenEntry marks a target the actor may not touch, its entry is in
	// another workspace, or outside the actor's department scope (CanAccessEntry).
	ErrForbiddenEntry = errors.New("crmbulk: entry is outside your workspace or department scope")
)

// I/O -------------------------------------------------------------------------

// EntryRef identifies one conversation entry to act on.
type EntryRef struct {
	EntryID   string `json:"entryId"`
	EntryType string `json:"entryType"`
}

// BulkInput is one bulk request: one Action + Value applied to every Target.
type BulkInput struct {
	WorkspaceID string
	ActorID     string
	IsAdmin     bool
	Action      string
	Targets     []EntryRef
	Value       string
}

// BulkFailure records a single target that could not be updated.
type BulkFailure struct {
	EntryID string `json:"entryId"`
	Error   string `json:"error"`
}

// BulkResult aggregates the fan-out outcome.
type BulkResult struct {
	Succeeded int           `json:"succeeded"`
	Failed    []BulkFailure `json:"failed"`
	// Forbidden is set when the actor lacks the ACTION's permission entirely (or the
	// action is unknown): a hard gate, no target is touched and the handler maps it
	// to 403. Per-target scope denials are reported in Failed instead.
	Forbidden bool `json:"forbidden,omitempty"`
}

// Service fans a single action out over many entries, enforcing the same RBAC +
// per-entry scope the single-entry paths do, and broadcasting each success.
type Service struct {
	stageAssigner StageAssigner
	labelAssigner LabelAssigner
	labelRemover  LabelRemover
	entryAssigner EntryAssigner
	authz         Authorizer
	broadcaster   Broadcaster
}

// NewService wires the single-entry operations the bulk fan-out reuses, plus the
// existing authorizer (RBAC + scope) and broadcaster.
func NewService(
	stageAssigner StageAssigner,
	labelAssigner LabelAssigner,
	labelRemover LabelRemover,
	entryAssigner EntryAssigner,
	authz Authorizer,
	broadcaster Broadcaster,
) *Service {
	return &Service{
		stageAssigner: stageAssigner,
		labelAssigner: labelAssigner,
		labelRemover:  labelRemover,
		entryAssigner: entryAssigner,
		authz:         authz,
		broadcaster:   broadcaster,
	}
}

// BulkApply enforces two gates, then fans the action out:
//   1. HARD RBAC gate (once): the actor must hold the action's permission (the same
//      one the single-entry route requires). Denied / unknown action → Forbidden,
//      nothing is touched (handler → 403).
//   2. PER-TARGET scope gate: CanAccessEntry blocks any entry in another workspace
//      or outside the actor's department scope, reported in Failed, never mutated.
// Each surviving target is mutated via the existing single-entry usecase and then
// broadcast, so every client stays in sync. One target never aborts the rest.
func (s *Service) BulkApply(ctx context.Context, in BulkInput) BulkResult {
	result := BulkResult{}

	resource, act, known := actionPermission(in.Action)
	// Unknown action or missing action-level permission is a hard denial: touch
	// nothing. Fail closed if no authorizer is wired.
	if !known || s.authz == nil ||
		!s.authz.HasWorkspacePermission(in.ActorID, in.WorkspaceID, resource, act, in.IsAdmin) {
		result.Forbidden = true
		return result
	}

	for _, t := range in.Targets {
		// Scope gate: cross-workspace AND out-of-department are both rejected here.
		if !s.authz.CanAccessEntry(in.ActorID, in.WorkspaceID, t.EntryID, t.EntryType, in.IsAdmin) {
			result.Failed = append(result.Failed, BulkFailure{EntryID: t.EntryID, Error: ErrForbiddenEntry.Error()})
			continue
		}
		if err := s.applyOne(ctx, in, t); err != nil {
			result.Failed = append(result.Failed, BulkFailure{EntryID: t.EntryID, Error: err.Error()})
			continue
		}
		s.broadcast(in, t)
		result.Succeeded++
	}
	return result
}

// broadcast emits the same realtime event the single-entry handler emits for this
// action, so a bulk change reaches every connected client. Only called AFTER a
// mutation succeeds. No-op when no broadcaster is wired (unit tests).
func (s *Service) broadcast(in BulkInput, t EntryRef) {
	if s.broadcaster == nil {
		return
	}
	switch in.Action {
	case ActionMoveStage:
		s.broadcaster.BroadcastStageUpdate(in.WorkspaceID, t.EntryID, t.EntryType)
	case ActionAddLabel, ActionRemoveLabel:
		s.broadcaster.BroadcastLabelUpdate(in.WorkspaceID, t.EntryID, t.EntryType)
	case ActionAssign:
		// Rebuilds the enriched entry (new responsável) from its id; nil message OK.
		s.broadcaster.BroadcastEntryUpdate(t.EntryID, t.EntryType, nil)
	}
}

// applyOne routes a single target to the matching single-entry usecase.
func (s *Service) applyOne(_ context.Context, in BulkInput, t EntryRef) error {
	switch in.Action {
	case ActionMoveStage:
		_, err := s.stageAssigner.Execute(in.WorkspaceID, stage.AssignEntryStageInput{
			StageID:   in.Value,
			EntryID:   t.EntryID,
			EntryType: t.EntryType,
		})
		return err

	case ActionAssign:
		// Manual reassignment carries no business phone context (mirrors the
		// hub's assign-to path, which defaults businessPhoneID to empty).
		return s.entryAssigner.Reassign(t.EntryID, t.EntryType, "", in.WorkspaceID, in.Value)

	case ActionAddLabel:
		_, err := s.labelAssigner.Execute(in.WorkspaceID, label.AssignEntryLabelInput{
			LabelID:   in.Value,
			EntryID:   t.EntryID,
			EntryType: t.EntryType,
		})
		return err

	case ActionRemoveLabel:
		return s.labelRemover.Execute(in.WorkspaceID, in.Value, t.EntryID, t.EntryType)

	default:
		return fmt.Errorf("%w: %q", ErrUnknownAction, in.Action)
	}
}
