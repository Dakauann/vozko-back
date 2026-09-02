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
	"vozko/domain/crmfilter"
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
	Execute(workspaceID string, input label.RemoveEntryLabelInput) error
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

// TargetResolver expands a CRM filter into the entries it matches.
//
// It is what lets a bulk action address "every conversation in stage X" instead
// of only the page the operator can see. It is deliberately a port over the SAME
// read path the CRM table renders from (the board's GetEntries), and that is the
// whole point: the rows the operator was looking at are the rows that change,
// under identical workspace and department scoping. Nothing here re-derives that
// query, so a filter can never mean one thing to the table and another to bulk.
//
// matched is the TOTAL the filter selects, which can exceed len(refs) — see
// MaxFilterTargets.
type TargetResolver interface {
	ResolveTargets(ctx context.Context, q TargetQuery) (refs []EntryRef, matched int64, err error)
}

// TargetQuery is a filter plus the actor's scope. The fields mirror the board's
// EntriesInput so the adapter in the composition root is a straight field copy
// rather than a translation with opinions of its own.
type TargetQuery struct {
	WorkspaceID          string
	ActorID              string
	IsAdmin              bool
	SelectedDepartmentID string
	Filter               crmfilter.Filter
	Limit                int
}

// MaxFilterTargets caps one filter-addressed bulk.
//
// A filter can match far more rows than anyone means to change with a single
// click, and the fan-out costs one write plus one broadcast per entry. The cap
// makes the worst case bounded; BulkResult.Truncated and .Matched then tell the
// operator that it bound and by how much, so a partial apply is never silent.
const MaxFilterTargets = 2000

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
	// ErrTargetResolverUnavailable means a filter-addressed bulk arrived but no
	// resolver is wired. Fail closed: acting on an unresolved filter would be
	// acting on the wrong set, and silently acting on nothing hides the misconfig.
	ErrTargetResolverUnavailable = errors.New("crmbulk: filter targeting is not available")
)

// I/O -------------------------------------------------------------------------

// EntryRef identifies one conversation entry to act on.
type EntryRef struct {
	EntryID   string `json:"entryId"`
	EntryType string `json:"entryType"`
}

// BulkInput is one bulk request: one Action + Value applied to every target.
//
// Targets are named one of two ways, never both. Explicit Targets are the
// operator's hand-picked rows. A Filter is "everything this view is showing" —
// the same filter the table was rendered with, expanded server-side so the
// action is not silently capped at whatever fit on one page. Targets wins when
// both are present, since a hand-picked set is the more specific intent.
type BulkInput struct {
	WorkspaceID string
	ActorID     string
	IsAdmin     bool
	Action      string
	Targets     []EntryRef
	Value       string

	// Filter expands to the targets when Targets is empty. Nil means "no filter
	// given", which is NOT the same as an empty filter: an empty Filter legitimately
	// selects the whole (scoped) workspace, so the distinction has to survive.
	Filter *crmfilter.Filter
	// SelectedDepartmentID scopes the expansion exactly as the table's own read
	// does, so bulk cannot reach a row the operator could not see.
	SelectedDepartmentID string
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
	// Matched is how many entries the filter selected, set only for a
	// filter-addressed bulk. With Truncated it lets the caller say "2000 of 5300
	// updated" rather than reporting a number the operator cannot interpret.
	Matched int64 `json:"matched,omitempty"`
	// Truncated reports that MaxFilterTargets bound and the rest were left alone.
	Truncated bool `json:"truncated,omitempty"`
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
	targets       TargetResolver
}

// SetTargetResolver enables filter-addressed bulk ("apply to everything this
// view matches").
//
// A setter rather than a constructor parameter because the capability is
// genuinely optional — every explicit-targets call works without it — and
// because the resolver bridges to another usecase, which only the composition
// root can supply without making this package depend on the board. Left unset,
// a filter-addressed request fails closed with ErrTargetResolverUnavailable.
func (s *Service) SetTargetResolver(r TargetResolver) {
	s.targets = r
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
//  1. HARD RBAC gate (once): the actor must hold the action's permission (the same
//     one the single-entry route requires). Denied / unknown action → Forbidden,
//     nothing is touched (handler → 403).
//  2. PER-TARGET scope gate: CanAccessEntry blocks any entry in another workspace
//     or outside the actor's department scope, reported in Failed, never mutated.
//
// Between them, a filter-addressed request is expanded into targets. The order
// matters: the RBAC gate runs FIRST, so an actor without the action's permission
// never causes a read of the set they cannot change.
//
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

	targets, err := s.resolveTargets(ctx, in, &result)
	if err != nil {
		result.Failed = append(result.Failed, BulkFailure{Error: err.Error()})
		return result
	}

	for _, t := range targets {
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

// resolveTargets settles WHICH entries this request is about.
//
// Explicit targets win: a hand-picked set is the more specific intent, and it
// costs no read. Otherwise the filter is expanded through the board's own read
// path, capped at MaxFilterTargets, with the cap reported on the result rather
// than swallowed.
//
// Neither form is a no-op, not an error: a bulk over an empty selection
// legitimately updates nothing. Judging a request MALFORMED stays at the edge,
// where the handler already rejects "no targets and no filter" with a 400 —
// duplicating that judgment here would give the same request two answers.
func (s *Service) resolveTargets(ctx context.Context, in BulkInput, result *BulkResult) ([]EntryRef, error) {
	if len(in.Targets) > 0 || in.Filter == nil {
		return in.Targets, nil
	}
	if s.targets == nil {
		return nil, ErrTargetResolverUnavailable
	}

	refs, matched, err := s.targets.ResolveTargets(ctx, TargetQuery{
		WorkspaceID:          in.WorkspaceID,
		ActorID:              in.ActorID,
		IsAdmin:              in.IsAdmin,
		SelectedDepartmentID: in.SelectedDepartmentID,
		Filter:               *in.Filter,
		Limit:                MaxFilterTargets,
	})
	if err != nil {
		return nil, fmt.Errorf("crmbulk: resolve filter targets: %w", err)
	}

	result.Matched = matched
	result.Truncated = matched > int64(len(refs))
	return refs, nil
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
//
// ActorID is passed on every input: the use case writes the entry's timeline
// event, and it can only name who did it if the caller says. This service
// deliberately logs nothing itself — a bulk move is many single moves, and the
// history each one leaves has to be the history a single move leaves.
func (s *Service) applyOne(_ context.Context, in BulkInput, t EntryRef) error {
	switch in.Action {
	case ActionMoveStage:
		_, err := s.stageAssigner.Execute(in.WorkspaceID, stage.AssignEntryStageInput{
			StageID:   in.Value,
			EntryID:   t.EntryID,
			EntryType: t.EntryType,
			ActorID:   in.ActorID,
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
			ActorID:   in.ActorID,
		})
		return err

	case ActionRemoveLabel:
		return s.labelRemover.Execute(in.WorkspaceID, label.RemoveEntryLabelInput{
			LabelID:   in.Value,
			EntryID:   t.EntryID,
			EntryType: t.EntryType,
			ActorID:   in.ActorID,
		})

	default:
		return fmt.Errorf("%w: %q", ErrUnknownAction, in.Action)
	}
}
