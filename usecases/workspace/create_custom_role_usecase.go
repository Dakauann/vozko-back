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

	if callerRole != "admin" {
		actor := mustBeMember(uc.repo, workspaceID, actorID)
		if actor == nil {
			return nil, workspace.ErrUnauthorized
		}
		if !actor.Role.CanManageMembers() {
			return nil, workspace.ErrInsufficientPermissions
		}
	}

	for _, pe := range input.Permissions {
		if !pe.Resource.IsValid() {
			return nil, workspace.ErrInvalidResource
		}
		if !pe.Action.IsValid() {
			return nil, workspace.ErrInvalidAction
		}
		if !workspace.ValidActionForResource(pe.Resource, pe.Action) {
			return nil, workspace.ErrInvalidAction
		}
	}

	input.Permissions = workspace.EnforceDependencies(input.Permissions)

	role := &workspace.CustomRole{
		ID:          uuid.New().String(),
		WorkspaceID: workspaceID,
		Name:        name,
		Description: strings.TrimSpace(input.Description),
		Permissions: input.Permissions,
	}

	if err := uc.roleRepo.CreateRole(role); err != nil {
		return nil, err
	}

	return role, nil
}
