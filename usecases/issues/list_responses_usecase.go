package issues_usecase

import "vozko/domain/issues/issue_response"

type listResponsesUseCase struct {
	repo issue_response.Repository
}

func NewListResponsesUseCase(repo issue_response.Repository) issue_response.ListResponsesUseCase {
	return &listResponsesUseCase{repo: repo}
}

func (uc *listResponsesUseCase) Execute(issueID string) ([]*issue_response.IssueResponse, error) {
	return uc.repo.ListByIssueID(issueID)
}
