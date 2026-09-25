package lead

import "vozko/domain/shared"

const MaxListPageSize = 200

type Queries interface {
	List(in ListLeadsInput) (*shared.PaginatedResult[*LeadWithSummary], error)
	Facets(in ListLeadsInput) (*LeadFacets, error)
	Get(workspaceID, id string) (*Lead, error)
	GetByNumber(workspaceID, number string) (*Lead, error)
}
