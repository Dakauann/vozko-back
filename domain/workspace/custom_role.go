package workspace

import "time"

type CustomRole struct {
	ID          string            `json:"id"`
	WorkspaceID string            `json:"workspaceId"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Permissions []PermissionEntry `json:"permissions"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

type CreateCustomRoleInput struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Permissions []PermissionEntry `json:"permissions"`
}

type UpdateCustomRoleInput struct {
	Name        *string           `json:"name,omitempty"`
	Description *string           `json:"description,omitempty"`
	Permissions []PermissionEntry `json:"permissions,omitempty"`
}

type CustomRoleRepository interface {
	CreateRole(role *CustomRole) error
	GetRoleByID(id string) (*CustomRole, error)
	ListRolesByWorkspace(workspaceID string) ([]*CustomRole, error)
	UpdateRole(role *CustomRole) error
	DeleteRole(id string) error
	ListMembersByRoleID(roleID string) ([]*Member, error)
}

type CreateCustomRoleUseCase interface {
	Execute(actorID, workspaceID, callerRole string, input CreateCustomRoleInput) (*CustomRole, error)
}

type ListCustomRolesUseCase interface {
	Execute(actorID, workspaceID, callerRole string) ([]*CustomRole, error)
}

type UpdateCustomRoleUseCase interface {
	Execute(actorID, workspaceID, callerRole, roleID string, input UpdateCustomRoleInput) (*CustomRole, error)
}

type DeleteCustomRoleUseCase interface {
	Execute(actorID, workspaceID, callerRole, roleID string) error
}

type AssignCustomRoleUseCase interface {
	Execute(actorID, workspaceID, memberUserID, callerRole, roleID string) (*Member, error)
}
