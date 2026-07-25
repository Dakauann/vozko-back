package issues

import "errors"

type IssueStatus string

const (
	IssueStatusOpen       IssueStatus = "open"
	IssueStatusInProgress IssueStatus = "in_progress"
	IssueStatusClosed     IssueStatus = "closed"
)

var (
	ErrIssueNotFound      = errors.New("issue not found")
	ErrIssueTitleRequired = errors.New("issue title is required")
	ErrIssueTitleTooLong  = errors.New("issue title must not exceed 255 characters")
	ErrIssueAlreadyClosed = errors.New("issue is already closed and cannot be reopened")
	ErrInvalidStatus      = errors.New("invalid issue status")
)

func (s IssueStatus) IsValid() bool {
	switch s {
	case IssueStatusOpen, IssueStatusInProgress, IssueStatusClosed:
		return true
	}
	return false
}

type Issue struct {
	ID          string      `json:"id"`
	WorkspaceID string      `json:"workspaceId"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	ImageURLs   []string    `json:"imageUrls,omitempty"`
	Status      IssueStatus `json:"status"`
	CreatedAt   int64       `json:"createdAt"`
	UpdatedAt   int64       `json:"updatedAt"`
	ClosedAt    *int64      `json:"closedAt,omitempty"`
}
