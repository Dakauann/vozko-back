package workspace_department

import (
	"context"

	"vozko/domain/working_hours"
)

type CreateDepartmentInput struct {
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type UpdateDepartmentInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	// WorkingHours sets this department's own schedule; nil means "not sent".
	// ClearWorkingHours removes it so the department inherits the workspace's
	// again — the distinction a pointer alone cannot carry. See the same pair on
	// workspace_config.UpdateWorkspaceConfigOwnerInput.
	WorkingHours      *working_hours.Spec `json:"workingHours,omitempty"`
	ClearWorkingHours bool                `json:"-"`
}

type AddMemberInput struct {
	MemberID string `json:"memberId"`
}

type CreateDepartmentUseCase interface {
	Execute(input CreateDepartmentInput) (*Department, error)
}

type GetDepartmentUseCase interface {
	Execute(id string) (*Department, error)
}

type ListDepartmentsUseCase interface {
	Execute(workspaceID string) ([]Department, error)
}

type ListDepartmentsByIDsUseCase interface {
	Execute(ids []string) ([]Department, error)
}

type UpdateDepartmentUseCase interface {
	Execute(id string, input UpdateDepartmentInput) (*Department, error)
}

type DeleteDepartmentUseCase interface {
	Execute(id string) error
}

type AddMemberUseCase interface {
	Execute(departmentID string, input AddMemberInput) (*DepartmentMember, error)
}

type RemoveMemberUseCase interface {
	Execute(departmentID, memberID string) error
}

type ListMembersUseCase interface {
	Execute(departmentID string) ([]DepartmentMember, error)
}

type CreationDepartmentResolver interface {
	Resolve(ctx context.Context, workspaceID string) (string, error)
}
