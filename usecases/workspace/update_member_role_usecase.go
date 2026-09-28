package workspace_usecase

import "vozko/domain/workspace"

type updateMemberRoleUseCase struct {
	repo workspace.Repository
}

func NewUpdateMemberRoleUseCase(repo workspace.Repository) workspace.UpdateMemberRoleUseCase {
	return &updateMemberRoleUseCase{repo: repo}
}

func (uc *updateMemberRoleUseCase) Execute(actorID, workspaceID, memberUserID, callerRole string, role workspace.Role) (*workspace.Member, error) {
	if !role.IsValid() || role == workspace.RoleOwner {
		return nil, workspace.ErrInvalidRole
	}

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

	if err := uc.repo.UpdateMemberRole(target.ID, role); err != nil {
		return nil, err
	}

	return uc.repo.GetMemberByID(target.ID)
}
