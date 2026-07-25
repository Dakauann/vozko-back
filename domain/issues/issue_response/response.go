package issue_response

import "errors"

var (
	ErrResponseNotFound    = errors.New("issue response not found")
	ErrResponseBodyEmpty   = errors.New("response body is required")
	ErrResponseBodyTooLong = errors.New("response body must not exceed 2000 characters")
	ErrIssueNotFound       = errors.New("issue not found")
	ErrNotAuthorized       = errors.New("not authorized to respond to issues")
)

type IssueResponse struct {
	ID        string  `json:"id"`
	IssueID   string  `json:"issueId"`
	AuthorID  string  `json:"authorId"`
	Body      string  `json:"body"`
	ImageURL  *string `json:"imageUrl,omitempty"`
	CreatedAt int64   `json:"createdAt"`
}
