package workspace

import (
	"strings"
	"time"
)

type CustomRole struct {
	ID          string            `json:"id"`
	WorkspaceID string            `json:"workspaceId"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Permissions []PermissionEntry `json:"permissions"`
	PresetKey   RolePresetKey     `json:"presetKey,omitempty"`
	Linked      bool              `json:"linked"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

type CreateCustomRoleInput struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Permissions []PermissionEntry `json:"permissions"`
	PresetKey   RolePresetKey     `json:"presetKey,omitempty"`
	Linked      bool              `json:"linked,omitempty"`
}

type UpdateCustomRoleInput struct {
	Name        *string           `json:"name,omitempty"`
	Description *string           `json:"description,omitempty"`
	Permissions []PermissionEntry `json:"permissions,omitempty"`
	Linked      *bool             `json:"linked,omitempty"`
}

type CustomRoleRepository interface {
	CreateRole(role *CustomRole) error
	GetRoleByID(id string) (*CustomRole, error)
	ListRolesByWorkspace(workspaceID string) ([]*CustomRole, error)
	UpdateRole(role *CustomRole) error
	DeleteRole(id string) error
	ListMembersByRoleID(roleID string) ([]*Member, error)
	ListLinkedRoles() ([]*CustomRole, error)
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

func RoleNameTaken(roles []*CustomRole, name, exceptID string) bool {
	wanted := strings.ToLower(strings.Join(strings.Fields(name), " "))
	for _, r := range roles {
		if r == nil || r.ID == exceptID {
			continue
		}
		if strings.ToLower(strings.Join(strings.Fields(r.Name), " ")) == wanted {
			return true
		}
	}
	return false
}
