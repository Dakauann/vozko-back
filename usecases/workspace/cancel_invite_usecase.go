package workspace_usecase

import (
	"vozko/domain/workspace"
)

type cancelInviteUseCase struct {
	repo workspace.Repository
}

func NewCancelInviteUseCase(repo workspace.Repository) workspace.CancelInviteUseCase {
	return &cancelInviteUseCase{repo: repo}
}

func (uc *cancelInviteUseCase) Execute(actorID, workspaceID, callerRole, inviteID string) error {
	invite, err := uc.repo.GetInviteByID(inviteID)
	if err != nil {
		return err
	}

	if invite.WorkspaceID != workspaceID {
		return workspace.ErrInviteNotFound
	}

	if err := requireManager(uc.repo, workspaceID, actorID, callerRole); err != nil {
		return err
	}

	if invite.Status != workspace.InviteStatusPending {
		return workspace.ErrInviteAlreadyProcessed
	}

	return uc.repo.UpdateInviteStatus(invite.ID, workspace.InviteStatusCancelled)
}
