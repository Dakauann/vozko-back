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
	if err := requireManager(uc.repo, workspaceID, actorID, callerRole); err != nil {
		return err
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
