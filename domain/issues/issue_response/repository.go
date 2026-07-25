package issue_response

type Repository interface {
	Create(resp *IssueResponse) error
	ListByIssueID(issueID string) ([]*IssueResponse, error)
}
