package workspace_usecase

import (
	"strings"

	"vozko/domain/workspace"
)

type updateCustomRoleUseCase struct {
	repo     workspace.Repository
	roleRepo workspace.CustomRoleRepository
}

func NewUpdateCustomRoleUseCase(repo workspace.Repository, roleRepo workspace.CustomRoleRepository) workspace.UpdateCustomRoleUseCase {
	return &updateCustomRoleUseCase{repo: repo, roleRepo: roleRepo}
}

func (uc *updateCustomRoleUseCase) Execute(actorID, workspaceID, callerRole, roleID string, input workspace.UpdateCustomRoleInput) (*workspace.CustomRole, error) {
	if err := requireManager(uc.repo, workspaceID, actorID, callerRole); err != nil {
		return nil, err
	}

	role, err := uc.roleRepo.GetRoleByID(roleID)
	if err != nil {
		return nil, err
	}
	if role.WorkspaceID != workspaceID {
		return nil, workspace.ErrRoleNotFound
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, workspace.ErrRoleNameRequired
		}
		if err := uniqueRoleName(uc.roleRepo, workspaceID, name, role.ID); err != nil {
			return nil, err
		}
		role.Name = name
	}
	if input.Description != nil {
		role.Description = strings.TrimSpace(*input.Description)
	}

	if input.Linked != nil {
		role.Linked = *input.Linked
	}
	if role.Linked && input.Permissions != nil {
		return nil, workspace.ErrLinkedRoleLocked
	}

	permissionsChanged := false
	if role.Linked {
		if permissionsChanged, err = role.SyncWithPreset(); err != nil {
			return nil, err
		}
	} else if input.Permissions != nil {
		permissions, err := validatedPermissions("update role "+roleID, input.Permissions)
		if err != nil {
			return nil, err
		}
		role.Permissions = permissions
		permissionsChanged = true
	}

	if err := uc.roleRepo.UpdateRole(role); err != nil {
		return nil, err
	}
	if permissionsChanged {
		if err := propagateRolePermissions(uc.repo, uc.roleRepo, role); err != nil {
			return role, err
		}
	}
	return role, nil
}
