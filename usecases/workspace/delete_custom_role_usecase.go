package workspace_usecase

import "vozko/domain/workspace"

type deleteCustomRoleUseCase struct {
	repo     workspace.Repository
	roleRepo workspace.CustomRoleRepository
}

func NewDeleteCustomRoleUseCase(repo workspace.Repository, roleRepo workspace.CustomRoleRepository) workspace.DeleteCustomRoleUseCase {
	return &deleteCustomRoleUseCase{repo: repo, roleRepo: roleRepo}
}

func (uc *deleteCustomRoleUseCase) Execute(actorID, workspaceID, callerRole, roleID string) error {
	if callerRole != "admin" {
		actor := mustBeMember(uc.repo, workspaceID, actorID)
		if actor == nil {
			return workspace.ErrUnauthorized
		}
		if !actor.Role.CanManageMembers() {
			return workspace.ErrInsufficientPermissions
		}
	}

	role, err := uc.roleRepo.GetRoleByID(roleID)
	if err != nil {
		return err
	}
	if role.WorkspaceID != workspaceID {
		return workspace.ErrRoleNotFound
	}

	members, err := uc.roleRepo.ListMembersByRoleID(roleID)
	if err != nil {
		return err
	}
	if len(members) > 0 {
		return workspace.ErrRoleInUse
	}

	return uc.roleRepo.DeleteRole(roleID)
}
