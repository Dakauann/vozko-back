package inbox_assignment

import (
	"errors"
	"time"

	"vozko/domain/actor"
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

func (a *InboxAssignment) AssignedTo(userID string) bool {
	return a != nil && a.AssignedUserID != "" && a.AssignedUserID == userID
}

// HeldByAutomation reports whether an AI agent (ai:<id>) or a workflow
// (workflow:<id>) holds the conversation rather than a person.
func (a *InboxAssignment) HeldByAutomation() bool {
	return a != nil && actor.IsAutomation(a.AssignedUserID)
}

func (a *InboxAssignment) VisibleTo(userID string) bool {
	return a == nil || a.AssignedUserID == "" || a.AssignedTo(userID)
}

type RoundRobinState struct {
	ID                 string    `json:"id"`
	WorkspaceID        string    `json:"workspaceId"`
	BusinessPhoneID    string    `json:"businessPhoneId"`
	DepartmentID       string    `json:"departmentId"`
	LastAssignedUserID string    `json:"lastAssignedUserId"`
	UpdatedAt          time.Time `json:"updatedAt"`
}
