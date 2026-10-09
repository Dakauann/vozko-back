package crmbulk_usecase

import (
	"context"
	"errors"
	"fmt"

	"vozko/domain/cache"
	"vozko/domain/conversation"
	"vozko/domain/crmfilter"
	"vozko/domain/label"
	"vozko/domain/selection"
	"vozko/domain/stage"
	"vozko/domain/workspace"
)

type StageAssigner interface {
	Execute(workspaceID string, input stage.AssignEntryStageInput) (*stage.EntryStage, error)
}

type LabelAssigner interface {
	Execute(workspaceID string, input label.AssignEntryLabelInput) (*label.EntryLabel, error)
}

type LabelRemover interface {
	Execute(workspaceID string, input label.RemoveEntryLabelInput) error
}

type EntryAssigner interface {
	Reassign(entryID, entryType, businessPhoneID, workspaceID, userID string) error
}

type Authorizer interface {
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
	EntryAccessFor(userID, workspaceID string, isAdmin bool) conversation.EntryAccess
}

type Broadcaster interface {
	BroadcastStageUpdate(workspaceID, entryID, entryType string)
	BroadcastLabelUpdate(workspaceID, entryID, entryType string)
	BroadcastEntryUpdate(entryID, entryType string, message *conversation.Message)
}

const MaxFilterTargets = 2000

const (
	ActionMoveStage   = "move_stage"
	ActionMoveFunnel  = "move_funnel"
	ActionAssign      = "assign"
	ActionAddLabel    = "add_label"
	ActionRemoveLabel = "remove_label"
)

var actionRequirements = map[string]workspace.PermissionEntry{
	ActionMoveStage:   {Resource: workspace.ResourceStages, Action: workspace.ActionAssign},
	ActionMoveFunnel:  {Resource: workspace.ResourceStages, Action: workspace.ActionTransfer},
	ActionAssign:      {Resource: workspace.ResourceConversations, Action: workspace.ActionAssign},
	ActionAddLabel:    {Resource: workspace.ResourceLabels, Action: workspace.ActionAssign},
	ActionRemoveLabel: {Resource: workspace.ResourceLabels, Action: workspace.ActionAssign},
}

func actionPermission(action string) (resource, act string, ok bool) {
	required, ok := actionRequirements[action]
	if !ok {
		return "", "", false
	}
	return string(required.Resource), string(required.Action), true
}

var (
	ErrUnknownAction  = errors.New("crmbulk: unknown action")
	ErrForbidden      = errors.New("crmbulk: you don't have permission to perform this bulk action")
	ErrForbiddenEntry = errors.New("crmbulk: entry is outside your workspace or department scope")
)

type EntryRef struct {
	EntryID   string `json:"entryId"`
	EntryType string `json:"entryType"`
}

type BulkInput struct {
	WorkspaceID string
	ActorID     string
	IsAdmin     bool
	Action      string
	Value       string

	Targets              []EntryRef
	Selection            selection.Selection
	SelectedDepartmentID string
}

type BulkFailure = selection.Failure

type BulkResult = selection.Result

type Service struct {
	stageAssigner StageAssigner
	labelAssigner LabelAssigner
	labelRemover  LabelRemover
	entryAssigner EntryAssigner
	authz         Authorizer
	broadcaster   Broadcaster
	targets       selection.Resolver
	countGate     cache.Gate
}

func (s *Service) SetSelection(resolver selection.Resolver, countGate cache.Gate) {
	s.targets = resolver
	s.countGate = countGate
}

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

func (s *Service) BulkApply(ctx context.Context, in BulkInput) (BulkResult, error) {
	result := BulkResult{}

	if err := s.authorize(in); err != nil {
		return result, err
	}

	targets, err := s.resolveTargets(ctx, in, &result)
	if err != nil {
		return result, err
	}

	result.Eligible = len(targets)
	access := s.authz.EntryAccessFor(in.ActorID, in.WorkspaceID, in.IsAdmin)
	if access == nil {
		return result, ErrForbidden
	}
	for _, t := range targets {
		if !access.CanAccess(t.EntryID, t.EntryType) {
			result.Fail(t.EntryID, ErrForbiddenEntry)
			continue
		}
		if err := s.applyOne(ctx, in, t); err != nil {
			result.Fail(t.EntryID, err)
			continue
		}
		s.broadcast(in, t)
		result.Succeeded++
	}
	return result, nil
}

func (s *Service) Count(ctx context.Context, scope selection.Scope, filter crmfilter.Filter) (int, string, error) {
	if err := filter.ValidateForSelection(); err != nil {
		return 0, "", err
	}
	counted := selection.ForFilter(filter)
	matched, err := s.count(ctx, scope, counted)
	if err != nil {
		return 0, "", err
	}
	return matched, counted.Fingerprint, nil
}

func (s *Service) authorize(in BulkInput) error {
	resource, act, known := actionPermission(in.Action)
	if !known {
		return fmt.Errorf("%w: %q", ErrUnknownAction, in.Action)
	}
	if s.authz == nil || !s.authz.HasWorkspacePermission(in.ActorID, in.WorkspaceID, resource, act, in.IsAdmin) {
		return ErrForbidden
	}
	return nil
}

func (s *Service) resolveTargets(ctx context.Context, in BulkInput, result *BulkResult) ([]EntryRef, error) {
	picked := in.Selection
	picked.IDs = entryIDs(in.Targets)
	picked = picked.Inferred()
	if err := picked.Validate(); err != nil {
		return nil, err
	}

	switch picked.Mode {
	case selection.ModeIDs:
		result.Matched = len(in.Targets)
		return in.Targets, nil
	case selection.ModeAllMatching, selection.ModeEveryone:
		return s.resolveMatching(ctx, in, picked, result)
	default:
		return nil, fmt.Errorf("%w: %q", selection.ErrModeUnsupported, picked.Mode)
	}
}

func (s *Service) gated(ctx context.Context, read func(context.Context) error) error {
	if s.targets == nil || s.countGate == nil {
		return selection.ErrResolverUnavailable
	}
	return cache.Gated(ctx, s.countGate, read)
}

func (s *Service) count(ctx context.Context, scope selection.Scope, picked selection.Selection) (int, error) {
	matched := 0
	err := s.gated(ctx, func(ctx context.Context) error {
		n, err := s.countSelection(ctx, scope, picked)
		matched = n
		return err
	})
	return matched, err
}

func (s *Service) countSelection(ctx context.Context, scope selection.Scope, picked selection.Selection) (int, error) {
	matched, err := s.targets.Count(ctx, scope, picked.BeforeExclusions())
	if err != nil {
		return 0, fmt.Errorf("crmbulk: count selection: %w", err)
	}
	return matched, nil
}

func (s *Service) resolveMatching(ctx context.Context, in BulkInput, picked selection.Selection, result *BulkResult) ([]EntryRef, error) {
	scope := selection.Scope{
		WorkspaceID:  in.WorkspaceID,
		ActorID:      in.ActorID,
		DepartmentID: in.SelectedDepartmentID,
		IsAdmin:      in.IsAdmin,
	}
	var resolved []selection.Ref
	err := s.gated(ctx, func(ctx context.Context) error {
		matched, err := s.countSelection(ctx, scope, picked)
		if err != nil {
			return err
		}
		if err := picked.ConfirmCount(matched); err != nil {
			return err
		}
		result.Matched = matched
		resolved, err = s.targets.Resolve(ctx, scope, picked, "", MaxFilterTargets+1)
		if err != nil {
			return fmt.Errorf("crmbulk: resolve selection: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	result.Truncated = len(resolved) > MaxFilterTargets
	resolved = resolved[:min(len(resolved), MaxFilterTargets)]
	refs := make([]EntryRef, 0, len(resolved))
	for _, ref := range resolved {
		refs = append(refs, EntryRef{EntryID: ref.ID, EntryType: ref.Type})
	}
	return refs, nil
}

func entryIDs(targets []EntryRef) []string {
	ids := make([]string, len(targets))
	for i, t := range targets {
		ids[i] = t.EntryID
	}
	return ids
}

func (s *Service) broadcast(in BulkInput, t EntryRef) {
	if s.broadcaster == nil {
		return
	}
	switch in.Action {
	case ActionMoveStage:
		s.broadcaster.BroadcastStageUpdate(in.WorkspaceID, t.EntryID, t.EntryType)
	case ActionAddLabel, ActionRemoveLabel:
		s.broadcaster.BroadcastLabelUpdate(in.WorkspaceID, t.EntryID, t.EntryType)
	}
}

func (s *Service) applyOne(_ context.Context, in BulkInput, t EntryRef) error {
	switch in.Action {
	case ActionMoveStage, ActionMoveFunnel:
		_, err := s.stageAssigner.Execute(in.WorkspaceID, stage.AssignEntryStageInput{
			StageID:            in.Value,
			EntryID:            t.EntryID,
			EntryType:          t.EntryType,
			ActorID:            in.ActorID,
			AllowCrossPipeline: in.Action == ActionMoveFunnel,
		})
		return err

	case ActionAssign:
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
