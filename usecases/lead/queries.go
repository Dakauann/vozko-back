package lead_usecase

import (
	"fmt"
	"strings"

	"vozko/domain/lead"
	"vozko/domain/shared"
)

type queries struct {
	repo lead.Repository
}

func NewQueries(repo lead.Repository) lead.Queries {
	return &queries{repo: repo}
}

func (q *queries) List(in lead.ListLeadsInput) (*shared.PaginatedResult[*lead.LeadWithSummary], error) {
	in, err := scoped(in)
	if err != nil {
		return nil, err
	}
	return q.repo.ListWithSummary(in)
}

func (q *queries) Facets(in lead.ListLeadsInput) (*lead.LeadFacets, error) {
	in, err := scoped(in)
	if err != nil {
		return nil, err
	}
	return q.repo.Facets(in)
}

func (q *queries) Get(workspaceID, id string) (*lead.Lead, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	return found(q.repo.FindByID(workspaceID, id))
}

func (q *queries) GetByNumber(workspaceID, number string) (*lead.Lead, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	if strings.TrimSpace(number) == "" {
		return nil, lead.ErrLeadRequired
	}
	return found(q.repo.FindByNumber(workspaceID, number))
}

func scoped(in lead.ListLeadsInput) (lead.ListLeadsInput, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return in, lead.ErrLeadWorkspaceRequired
	}
	if in.Options.Pagination.PageSize > lead.MaxListPageSize {
		in.Options.Pagination.PageSize = lead.MaxListPageSize
	}
	return in, nil
}

func found(l *lead.Lead, err error) (*lead.Lead, error) {
	if err != nil {
		return nil, fmt.Errorf("find lead: %w", err)
	}
	if l == nil {
		return nil, lead.ErrLeadNotFound
	}
	return l, nil
}
