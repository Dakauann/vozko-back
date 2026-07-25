package workspace_usecase

import "vozko/domain/workspace"

type removeMemberUseCase struct {
	repo workspace.Repository
}

func NewRemoveMemberUseCase(repo workspace.Repository) workspace.RemoveMemberUseCase {
	return &removeMemberUseCase{repo: repo}
}

func (uc *removeMemberUseCase) Execute(actorID, workspaceID, memberUserID, callerRole string) error {
	var actor *workspace.Member
	if callerRole != "admin" {
		actor = mustBeMember(uc.repo, workspaceID, actorID)
		if actor == nil {
			return workspace.ErrUnauthorized
		}
	}

	target, err := uc.repo.GetMember(workspaceID, memberUserID)
	if err != nil {
		return err
	}
	if target == nil {
		return workspace.ErrMemberNotFound
	}

	if target.Role == workspace.RoleOwner {
		return workspace.ErrCannotRemoveOwner
	}

	if actor != nil && target.Role == workspace.RoleAdmin && !actor.Role.CanManageMembers() {
		return workspace.ErrInsufficientPermissions
	}

	return uc.repo.RemoveMember(target.ID)
}
