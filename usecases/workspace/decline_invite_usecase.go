package workspace_usecase

import (
	"strings"

	"vozko/domain/workspace"
)

type declineInviteUseCase struct {
	repo workspace.Repository
}

func NewDeclineInviteUseCase(repo workspace.Repository) workspace.DeclineInviteUseCase {
	return &declineInviteUseCase{repo: repo}
}

func (uc *declineInviteUseCase) Execute(userID, userEmail, inviteID string) error {
	invite, err := uc.repo.GetInviteByID(inviteID)
	if err != nil {
		return err
	}

	if !strings.EqualFold(strings.TrimSpace(userEmail), strings.TrimSpace(invite.Email)) {
		return workspace.ErrUnauthorized
	}

	if invite.Status != workspace.InviteStatusPending {
		return workspace.ErrInviteAlreadyProcessed
	}

	return uc.repo.UpdateInviteStatus(invite.ID, workspace.InviteStatusDeclined)
}
