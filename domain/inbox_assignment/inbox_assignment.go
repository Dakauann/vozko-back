package inbox_assignment

import (
	"errors"
	"time"
)

var (
	ErrAssignmentNotFound = errors.New("inbox assignment: not found")
)

type InboxAssignment struct {
	ID              string    `json:"id"`
	WorkspaceID     string    `json:"workspaceId"`
	BusinessPhoneID string    `json:"businessPhoneId"`
	EntryID         string    `json:"entryId"`
	EntryType       string    `json:"entryType"`
	AssignedUserID  string    `json:"assignedUserId"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type RoundRobinState struct {
	ID                 string    `json:"id"`
	WorkspaceID        string    `json:"workspaceId"`
	BusinessPhoneID    string    `json:"businessPhoneId"`
	DepartmentID       string    `json:"departmentId"`
	LastAssignedUserID string    `json:"lastAssignedUserId"`
	UpdatedAt          time.Time `json:"updatedAt"`
}
