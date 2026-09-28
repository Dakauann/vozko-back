package workspace_usecase

import "vozko/domain/workspace"

type removeMemberUseCase struct {
	repo workspace.Repository
}

func NewRemoveMemberUseCase(repo workspace.Repository) workspace.RemoveMemberUseCase {
	return &removeMemberUseCase{repo: repo}
}

func (uc *removeMemberUseCase) Execute(actorID, workspaceID, memberUserID, callerRole string) error {
	actor, err := actorFor(uc.repo, workspaceID, actorID, callerRole)
	if err != nil {
		return err
	}

	target, err := uc.repo.GetMember(workspaceID, memberUserID)
	if err != nil {
		return err
	}
	if target == nil {
		return workspace.ErrMemberNotFound
	}

	if err := actor.CanRemove(target); err != nil {
		return err
	}

	return uc.repo.RemoveMember(target.ID)
}
