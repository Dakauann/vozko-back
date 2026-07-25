package issues_usecase

import (
	"vozko/domain/issues"
	"vozko/domain/shared"
)

type listIssuesUseCase struct {
	repo issues.Repository
}

func NewListIssuesUseCase(repo issues.Repository) issues.ListIssuesUseCase {
	return &listIssuesUseCase{repo: repo}
}

func (uc *listIssuesUseCase) Execute(input issues.ListIssuesInput) (*shared.PaginatedResult[*issues.Issue], error) {
	return uc.repo.List(input)
}
