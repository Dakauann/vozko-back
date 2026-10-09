package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/domain/selection"
	"vozko/domain/workspace"
)

var errSelectionIncomplete = errors.New("lead selection: a required dependency is missing")

type SelectionDeps struct {
	Reader      lead.SelectionReader
	Snapshots   lead.SelectionSnapshots
	Permissions Permissions
	Definitions DefinitionSource
	Zones       WorkspaceZones
	Areas       AreaFilters
	Now         func() time.Time
}

type SelectionResolver struct {
	deps    SelectionDeps
	viewers viewers
	binding filterBinding
}

func NewSelectionResolver(deps SelectionDeps) (*SelectionResolver, error) {
	missing := map[string]bool{
		"reader":      deps.Reader == nil,
		"snapshots":   deps.Snapshots == nil,
		"permissions": deps.Permissions == nil,
		"definitions": deps.Definitions == nil,
		"zones":       deps.Zones == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errSelectionIncomplete, name)
		}
	}
	return &SelectionResolver{
		deps:    deps,
		viewers: viewers{permissions: deps.Permissions, definitions: deps.Definitions},
		binding: filterBinding{areas: deps.Areas, zones: deps.Zones, now: deps.Now},
	}, nil
}

func (r *SelectionResolver) Count(ctx context.Context, scope selection.Scope, s selection.Selection) (int, error) {
	counted := s.BeforeExclusions()
	counted.Require, counted.Limit, counted.Sort = nil, 0, nil
	if counted.Mode == selection.ModeFirstN {
		counted.Mode = selection.ModeAllMatching
	}
	q, err := r.query(ctx, scope, counted)
	if err != nil {
		return 0, err
	}
	return r.deps.Reader.CountSelection(ctx, q)
}

func (r *SelectionResolver) Resolve(ctx context.Context, scope selection.Scope, s selection.Selection, after string, limit int) ([]selection.Ref, error) {
	if err := selection.ValidatePage(limit); err != nil {
		return nil, err
	}
	q, err := r.query(ctx, scope, s)
	if err != nil {
		return nil, err
	}
	ids, err := r.deps.Reader.SelectionPage(ctx, q, after, limit)
	if err != nil {
		return nil, err
	}
	refs := make([]selection.Ref, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, selection.Ref{ID: id, Type: lead.SelectionRefType})
	}
	return refs, nil
}

func (r *SelectionResolver) Selected(ctx context.Context, scope selection.Scope, s selection.Selection) (int, error) {
	q, err := r.query(ctx, scope, s)
	if err != nil {
		return 0, err
	}
	limit := q.Limit
	q.Limit, q.Order = 0, nil
	n, err := r.deps.Reader.CountSelection(ctx, q)
	if err != nil {
		return 0, err
	}
	if limit > 0 {
		return min(n, limit), nil
	}
	return n, nil
}

func (r *SelectionResolver) Freeze(ctx context.Context, scope selection.Scope, s selection.Selection, snapshotID string, pending *lead.Assignment) (lead.Frozen, error) {
	q, err := r.query(ctx, scope, s)
	if err != nil {
		return lead.Frozen{}, err
	}
	q.Pending = pending
	return r.deps.Snapshots.FreezeSelection(ctx, q, snapshotID)
}

func (r *SelectionResolver) SnapshotSize(ctx context.Context, workspaceID, snapshotID string) (int, error) {
	return r.deps.Snapshots.SnapshotSize(ctx, workspaceID, snapshotID)
}

func (r *SelectionResolver) Snapshot(ctx context.Context, workspaceID, snapshotID, after string, limit int) ([]string, error) {
	if err := selection.ValidatePage(limit); err != nil {
		return nil, err
	}
	return r.deps.Snapshots.SnapshotPage(ctx, workspaceID, snapshotID, after, limit)
}

func (r *SelectionResolver) DropSnapshot(ctx context.Context, workspaceID, snapshotID string) error {
	return r.deps.Snapshots.DropSnapshot(ctx, workspaceID, snapshotID)
}

func (r *SelectionResolver) SweepSnapshots(ctx context.Context, before time.Time, limit int) (int64, error) {
	return r.deps.Snapshots.DropSnapshotsBefore(ctx, before, limit)
}

func (r *SelectionResolver) query(ctx context.Context, scope selection.Scope, s selection.Selection) (lead.SelectionQuery, error) {
	a := Actor{WorkspaceID: strings.TrimSpace(scope.WorkspaceID), UserID: scope.ActorID, IsAdmin: scope.IsAdmin}
	if a.WorkspaceID == "" {
		return lead.SelectionQuery{}, lead.ErrLeadWorkspaceRequired
	}
	if !r.viewers.allowed(a, workspace.ActionRead) {
		return lead.SelectionQuery{}, lead.ErrLeadForbidden
	}
	v, err := r.viewers.of(a)
	if err != nil {
		return lead.SelectionQuery{}, err
	}
	bound, err := r.binding.bind(ctx, a, v, s.EffectiveFilter())
	if err != nil {
		return lead.SelectionQuery{}, err
	}
	q := lead.SelectionQuery{WorkspaceID: a.WorkspaceID, Filter: bound.filter, Today: bound.today}
	if q.ExcludeIDs, err = lead.SelectionIDs(s.ExcludeIDs); err != nil {
		return lead.SelectionQuery{}, err
	}
	if s.Require != nil {
		q.Require = *s.Require
	}
	switch s.Mode {
	case selection.ModeIDs:
		if q.IDs, err = lead.SelectionIDs(s.IDs); err != nil {
			return lead.SelectionQuery{}, err
		}
		if len(q.IDs) == 0 {
			return lead.SelectionQuery{}, selection.ErrEmptySelection
		}
	case selection.ModeFirstN:
		if q.Order, err = lead.SelectionOrder(s.Sort); err != nil {
			return lead.SelectionQuery{}, err
		}
		q.Limit = s.Limit
	case selection.ModeAllMatching, selection.ModeEveryone:
	default:
		return lead.SelectionQuery{}, fmt.Errorf("%w: %q", selection.ErrUnknownMode, s.Mode)
	}
	if q.Require.UsesField(crmfilter.FieldBirthday) && q.Today.IsZero() {
		if q.Today, err = r.binding.today(ctx, a.WorkspaceID); err != nil {
			return lead.SelectionQuery{}, err
		}
	}
	return q, nil
}

var _ selection.Resolver = (*SelectionResolver)(nil)
