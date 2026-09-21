package workspace_department

import (
	"time"

	"vozko/domain/working_hours"
)

type Department struct {
	ID           string              `json:"id"`
	WorkspaceID  string              `json:"workspaceId"`
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	MemberCount  int                 `json:"memberCount,omitempty"`
	WorkingHours *working_hours.Spec `json:"workingHours,omitempty"`
	CreatedAt    time.Time           `json:"createdAt"`
	UpdatedAt    time.Time           `json:"updatedAt"`
}

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
