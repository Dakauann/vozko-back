package crmboard_usecase

import (
	"context"
	"errors"
	"fmt"

	"vozko/domain/selection"
)

type SelectionResolver struct {
	board *Service
}

func NewSelectionResolver(board *Service) SelectionResolver {
	return SelectionResolver{board: board}
}

func (r SelectionResolver) Count(_ context.Context, scope selection.Scope, s selection.Selection) (int, error) {
	if r.board == nil {
		return 0, selection.ErrResolverUnavailable
	}
	total, err := r.board.CountEntries(entriesFor(scope, s))
	if err != nil {
		return 0, asSelectionError(err)
	}
	return int(total), nil
}

func (r SelectionResolver) Resolve(_ context.Context, scope selection.Scope, s selection.Selection, after string, limit int) ([]selection.Ref, error) {
	if r.board == nil {
		return nil, selection.ErrResolverUnavailable
	}
	entries, err := r.board.ResolveEntryRefs(entriesFor(scope, s), after, limit)
	if err != nil {
		return nil, asSelectionError(err)
	}
	refs := make([]selection.Ref, 0, len(entries))
	for _, e := range entries {
		refs = append(refs, selection.Ref{ID: e.EntryID, Type: string(e.EntryType)})
	}
	return refs, nil
}

func entriesFor(scope selection.Scope, s selection.Selection) EntriesInput {
	return EntriesInput{
		WorkspaceID:          scope.WorkspaceID,
		UserID:               scope.ActorID,
		IsAdmin:              scope.IsAdmin,
		SelectedDepartmentID: scope.DepartmentID,
		Filter:               s.EffectiveFilter(),
		ExcludeEntryIDs:      s.ExcludeIDs,
	}
}

func asSelectionError(err error) error {
	if errors.Is(err, ErrUnauthorized) {
		return fmt.Errorf("%w: %w", selection.ErrScopeDenied, err)
	}
	return err
}
