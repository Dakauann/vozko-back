package issue_response

type CreateResponseUseCase interface {
	Execute(issueID, authorID, body string, imageURL *string) (*IssueResponse, error)
}

type ListResponsesUseCase interface {
	Execute(issueID string) ([]*IssueResponse, error)
}
