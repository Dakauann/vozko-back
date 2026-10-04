package workspace_usecase

import (
	"strings"

	"github.com/google/uuid"

	"vozko/domain/workspace"
)

type createCustomRoleUseCase struct {
	repo     workspace.Repository
	roleRepo workspace.CustomRoleRepository
}

func NewCreateCustomRoleUseCase(repo workspace.Repository, roleRepo workspace.CustomRoleRepository) workspace.CreateCustomRoleUseCase {
	return &createCustomRoleUseCase{repo: repo, roleRepo: roleRepo}
}

func (uc *createCustomRoleUseCase) Execute(actorID, workspaceID, callerRole string, input workspace.CreateCustomRoleInput) (*workspace.CustomRole, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, workspace.ErrRoleNameRequired
	}

	if err := requireManager(uc.repo, workspaceID, actorID, callerRole); err != nil {
		return nil, err
	}
	if err := uniqueRoleName(uc.roleRepo, workspaceID, name, ""); err != nil {
		return nil, err
	}

	role := &workspace.CustomRole{
		ID:          uuid.New().String(),
		WorkspaceID: workspaceID,
		Name:        name,
		Description: strings.TrimSpace(input.Description),
		PresetKey:   input.PresetKey,
		Linked:      input.Linked,
	}
	if err := role.ValidatePreset(); err != nil {
		return nil, err
	}
	if role.Linked {
		if _, err := role.SyncWithPreset(); err != nil {
			return nil, err
		}
	} else {
		permissions, err := validatedPermissions("create role in workspace "+workspaceID, input.Permissions)
		if err != nil {
			return nil, err
		}
		role.Permissions = permissions
	}

	if err := uc.roleRepo.CreateRole(role); err != nil {
		return nil, err
	}
	return role, nil
}
