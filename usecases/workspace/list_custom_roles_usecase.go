package workspace_usecase

import "vozko/domain/workspace"

type listCustomRolesUseCase struct {
	repo     workspace.Repository
	roleRepo workspace.CustomRoleRepository
}

func NewListCustomRolesUseCase(repo workspace.Repository, roleRepo workspace.CustomRoleRepository) workspace.ListCustomRolesUseCase {
	return &listCustomRolesUseCase{repo: repo, roleRepo: roleRepo}
}

func (uc *listCustomRolesUseCase) Execute(actorID, workspaceID, callerRole string) ([]*workspace.CustomRole, error) {
	if callerRole != "admin" {
		actor := mustBeMember(uc.repo, workspaceID, actorID)
		if actor == nil {
			return nil, workspace.ErrUnauthorized
		}
	}

	return uc.roleRepo.ListRolesByWorkspace(workspaceID)
}
