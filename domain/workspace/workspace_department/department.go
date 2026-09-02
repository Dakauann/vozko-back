package workspace_department

import (
	"time"

	"vozko/domain/working_hours"
)

type Department struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MemberCount int    `json:"memberCount,omitempty"`
	// WorkingHours overrides the workspace schedule for conversations that
	// belong to this department. Nil means it inherits.
	WorkingHours *working_hours.Spec `json:"workingHours,omitempty"`
	CreatedAt    time.Time           `json:"createdAt"`
	UpdatedAt    time.Time           `json:"updatedAt"`
}

// DepartmentSchedule is one department's own hours, as the rescue sweep needs
// them: department id, the workspace it belongs to, and the policy.
//
// The sweep reads every override for its eligible workspaces in ONE query per
// tick rather than a lookup per stalled conversation. That is also what lets it
// decide, before the candidate query runs, whether a workspace can have
// anything due at all.
type DepartmentSchedule struct {
	DepartmentID string
	WorkspaceID  string
	WorkingHours *working_hours.Spec
}

type DepartmentMember struct {
	ID           string    `json:"id"`
	DepartmentID string    `json:"departmentId"`
	MemberID     string    `json:"memberId"`
	UserID       string    `json:"userId,omitempty"`
	Email        string    `json:"email,omitempty"`
	Username     string    `json:"username,omitempty"`
	Role         string    `json:"role,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}
