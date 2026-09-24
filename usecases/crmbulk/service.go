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
	CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool
}

type Broadcaster interface {
	BroadcastStageUpdate(workspaceID, entryID, entryType string)
	BroadcastLabelUpdate(workspaceID, entryID, entryType string)
	BroadcastEntryUpdate(entryID, entryType string, message *conversation.Message)
}

type TargetResolver interface {
	ResolveTargets(ctx context.Context, q TargetQuery) (refs []EntryRef, matched int64, err error)
}

type TargetQuery struct {
	WorkspaceID          string
	ActorID              string
	IsAdmin              bool
	SelectedDepartmentID string
	Filter               crmfilter.Filter
	Limit                int
}

const MaxFilterTargets = 2000

func actionPermission(action string) (resource, act string, ok bool) {
	switch action {
	case ActionMoveStage:
		return string(workspace.ResourceStages), string(workspace.ActionAssign), true
	case ActionMoveFunnel:
		return string(workspace.ResourceStages), string(workspace.ActionTransfer), true
	case ActionAssign:
		return string(workspace.ResourceConversations), string(workspace.ActionAssign), true
	case ActionAddLabel, ActionRemoveLabel:
		return string(workspace.ResourceLabels), string(workspace.ActionAssign), true
	default:
		return "", "", false
	}
}

const (
	ActionMoveStage   = "move_stage"
	ActionMoveFunnel  = "move_funnel"
	ActionAssign      = "assign"
	ActionAddLabel    = "add_label"
	ActionRemoveLabel = "remove_label"
)

var (
	ErrUnknownAction             = errors.New("crmbulk: unknown action")
	ErrForbiddenEntry            = errors.New("crmbulk: entry is outside your workspace or department scope")
	ErrTargetResolverUnavailable = errors.New("crmbulk: filter targeting is not available")
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
	Targets     []EntryRef
	Value       string

	Filter               *crmfilter.Filter
	SelectedDepartmentID string
}

type BulkFailure struct {
	EntryID string `json:"entryId"`
	Error   string `json:"error"`
}

type BulkResult struct {
	Succeeded int           `json:"succeeded"`
	Failed    []BulkFailure `json:"failed"`
	Forbidden bool          `json:"forbidden,omitempty"`
	Matched   int64         `json:"matched,omitempty"`
	Truncated bool          `json:"truncated,omitempty"`
}

type Service struct {
	stageAssigner StageAssigner
	labelAssigner LabelAssigner
	labelRemover  LabelRemover
	entryAssigner EntryAssigner
	authz         Authorizer
	broadcaster   Broadcaster
	targets       TargetResolver
}

func (s *Service) SetTargetResolver(r TargetResolver) {
	s.targets = r
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

func (s *Service) BulkApply(ctx context.Context, in BulkInput) BulkResult {
	result := BulkResult{}

	resource, act, known := actionPermission(in.Action)
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

func (s *Service) broadcast(in BulkInput, t EntryRef) {
	if s.broadcaster == nil {
		return
	}
	switch in.Action {
	case ActionMoveStage:
		s.broadcaster.BroadcastStageUpdate(in.WorkspaceID, t.EntryID, t.EntryType)
	case ActionAddLabel, ActionRemoveLabel:
		s.broadcaster.BroadcastLabelUpdate(in.WorkspaceID, t.EntryID, t.EntryType)
		// A reassignment is announced by the assignment service, which also tells
		// whoever lost the conversation.
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
