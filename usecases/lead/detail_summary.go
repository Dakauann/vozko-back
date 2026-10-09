package lead_usecase

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sync/errgroup"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/opportunity"
	"vozko/domain/workspace"
)

var errSummaryIncomplete = errors.New("lead summary: a required dependency is missing")

type LeadPhones interface {
	LoadForDial(ctx context.Context, workspaceID, leadID string) (*lead.Lead, error)
}

type MemoryCounter interface {
	CountMemoriesOfLead(ctx context.Context, workspaceID, leadID string) (int, error)
}

type SummaryDeps struct {
	Permissions Permissions
	Leads       LeadPhones
	DealScopes  DealScopes
	Deals       opportunity.LeadDealCounter
	Memories    MemoryCounter
	Holders     lead.NumberHolderReader
}

type DetailSummaries struct {
	deps    SummaryDeps
	viewers viewers
}

type LeadDetailSummary struct {
	DealsCount    *int
	MemoriesCount int
	SharedNumbers []lead.SharedNumber
}

func NewDetailSummaries(deps SummaryDeps) (*DetailSummaries, error) {
	missing := map[string]bool{
		"permissions":    deps.Permissions == nil,
		"leads":          deps.Leads == nil,
		"deal scopes":    deps.DealScopes == nil,
		"deal counts":    deps.Deals == nil,
		"memory counts":  deps.Memories == nil,
		"number holders": deps.Holders == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errSummaryIncomplete, name)
		}
	}
	return &DetailSummaries{deps: deps, viewers: viewers{permissions: deps.Permissions}}, nil
}

func (s *DetailSummaries) Summary(ctx context.Context, v conversation.Viewer, leadID string) (LeadDetailSummary, error) {
	if err := requireLeadRef(v, leadID); err != nil {
		return LeadDetailSummary{}, err
	}
	if !s.viewers.allowed(v, workspace.ActionRead) {
		return LeadDetailSummary{}, lead.ErrLeadForbidden
	}
	l, err := found(s.deps.Leads.LoadForDial(ctx, v.WorkspaceID, leadID))
	if err != nil {
		return LeadDetailSummary{}, err
	}
	var summary LeadDetailSummary
	var holders []*lead.Lead
	reads, readCtx := errgroup.WithContext(ctx)
	reads.Go(func() error {
		count, err := s.dealsCount(readCtx, v, l.ID)
		summary.DealsCount = count
		return err
	})
	reads.Go(func() error {
		count, err := s.deps.Memories.CountMemoriesOfLead(readCtx, v.WorkspaceID, l.ID)
		if err != nil {
			return fmt.Errorf("memories of lead %s: %w", l.ID, err)
		}
		summary.MemoriesCount = count
		return nil
	})
	reads.Go(func() error {
		found, err := s.deps.Holders.OtherHolders(readCtx, v.WorkspaceID, l.ID, l.Numbers())
		if err != nil {
			return fmt.Errorf("other holders of the numbers of lead %s: %w", l.ID, err)
		}
		holders = found
		return nil
	})
	if err := reads.Wait(); err != nil {
		return LeadDetailSummary{}, err
	}
	summary.SharedNumbers = lead.SharedNumbersOf(l, holders)
	return summary, nil
}

func (s *DetailSummaries) dealsCount(ctx context.Context, v conversation.Viewer, leadID string) (*int, error) {
	scope, err := s.deps.DealScopes.Scope(personOf(v), v.WorkspaceID)
	if errors.Is(err, opportunity.ErrScopeDenied) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("deal scope: %w", err)
	}
	count, err := s.deps.Deals.CountDealsOfLead(ctx, v.WorkspaceID, leadID, scope)
	if err != nil {
		return nil, fmt.Errorf("deals of lead %s: %w", leadID, err)
	}
	return &count, nil
}
