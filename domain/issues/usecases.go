package issues

import "vozko/domain/shared"

type CreateIssueUseCase interface {
	Execute(workspaceID, title, description string, imageURLs []string) (*Issue, error)
}

type ListIssuesUseCase interface {
	Execute(input ListIssuesInput) (*shared.PaginatedResult[*Issue], error)
}

type ListAllIssuesUseCase interface {
	Execute(options shared.QueryOptions) (*shared.PaginatedResult[*Issue], error)
}

type GetIssueUseCase interface {
	Execute(id string) (*Issue, error)
}

type CloseIssueUseCase interface {
	Execute(id string) (*Issue, error)
}

type UpdateIssueStatusUseCase interface {
	Execute(id string, status IssueStatus) (*Issue, error)
}
