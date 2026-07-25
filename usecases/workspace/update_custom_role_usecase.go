package workspace_usecase

import (
	"strings"

	"github.com/google/uuid"

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
	if callerRole != "admin" {
		actor := mustBeMember(uc.repo, workspaceID, actorID)
		if actor == nil {
			return nil, workspace.ErrUnauthorized
		}
		if !actor.Role.CanManageMembers() {
			return nil, workspace.ErrInsufficientPermissions
		}
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
		role.Name = name
	}
	if input.Description != nil {
		role.Description = strings.TrimSpace(*input.Description)
	}

	permissionsChanged := false
	if input.Permissions != nil {

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
		role.Permissions = workspace.EnforceDependencies(input.Permissions)
		permissionsChanged = true
	}

	if err := uc.roleRepo.UpdateRole(role); err != nil {
		return nil, err
	}

	if permissionsChanged {
		members, err := uc.roleRepo.ListMembersByRoleID(roleID)
		if err != nil {
			return role, nil
		}
		for _, m := range members {
			perms := make([]*workspace.Permission, len(role.Permissions))
			for i, pe := range role.Permissions {
				perms[i] = &workspace.Permission{
					ID:       uuid.New().String(),
					MemberID: m.ID,
					Resource: pe.Resource,
					Action:   pe.Action,
				}
			}
			_ = uc.repo.SetPermissions(m.ID, perms)
		}
	}

	return role, nil
}
