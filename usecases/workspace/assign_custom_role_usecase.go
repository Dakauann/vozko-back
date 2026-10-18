package workspace_usecase

import (
	"vozko/domain/workspace"
)

type assignCustomRoleUseCase struct {
	repo     workspace.Repository
	roleRepo workspace.CustomRoleRepository
}

func NewAssignCustomRoleUseCase(repo workspace.Repository, roleRepo workspace.CustomRoleRepository) workspace.AssignCustomRoleUseCase {
	return &assignCustomRoleUseCase{repo: repo, roleRepo: roleRepo}
}

func (uc *assignCustomRoleUseCase) Execute(actorID, workspaceID, memberUserID, callerRole, roleID string) (*workspace.Member, error) {

	actor, err := actorFor(uc.repo, workspaceID, actorID, callerRole)
	if err != nil {
		return nil, err
	}
	if err := actor.ManagesMembers(); err != nil {
		return nil, err
	}

	target, err := uc.repo.GetMember(workspaceID, memberUserID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, workspace.ErrMemberNotFound
	}

	if err := actor.CanReassign(target); err != nil {
		return nil, err
	}

	role, err := uc.roleRepo.GetRoleByID(roleID)
	if err != nil {
		return nil, err
	}
	if role.WorkspaceID != workspaceID {
		return nil, workspace.ErrRoleNotFound
	}

	if err := uc.repo.UpdateMemberRoleID(target.ID, roleID); err != nil {
		return nil, err
	}

	if err := uc.repo.SetPermissions(target.ID, memberGrants(target.ID, role.Permissions)); err != nil {
		return nil, err
	}

	return uc.repo.GetMemberByID(target.ID)
}
