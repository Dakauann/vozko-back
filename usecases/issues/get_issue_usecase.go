package issues_usecase

import "vozko/domain/issues"

type getIssueUseCase struct {
	repo issues.Repository
}

func NewGetIssueUseCase(repo issues.Repository) issues.GetIssueUseCase {
	return &getIssueUseCase{repo: repo}
}

func (uc *getIssueUseCase) Execute(id string) (*issues.Issue, error) {
	return uc.repo.GetByID(id)
}
