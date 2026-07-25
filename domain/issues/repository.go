package issues

import "vozko/domain/shared"

type ListIssuesInput struct {
	WorkspaceID string
	Options     shared.QueryOptions
}

type Repository interface {
	Create(issue *Issue) error
	GetByID(id string) (*Issue, error)
	List(input ListIssuesInput) (*shared.PaginatedResult[*Issue], error)
	ListAll(options shared.QueryOptions) (*shared.PaginatedResult[*Issue], error)
	UpdateStatus(id string, newStatus IssueStatus) error
}
