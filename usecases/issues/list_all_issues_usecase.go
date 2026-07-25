package issues_usecase

import (
	"vozko/domain/issues"
	"vozko/domain/shared"
)

type listAllIssuesUseCase struct {
	repo issues.Repository
}

func NewListAllIssuesUseCase(repo issues.Repository) issues.ListAllIssuesUseCase {
	return &listAllIssuesUseCase{repo: repo}
}

func (uc *listAllIssuesUseCase) Execute(options shared.QueryOptions) (*shared.PaginatedResult[*issues.Issue], error) {
	return uc.repo.ListAll(options)
}
