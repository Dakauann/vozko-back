package workspace_usecase

import "vozko/domain/workspace"

type listInvitesUseCase struct {
	repo workspace.Repository
}

func NewListInvitesUseCase(repo workspace.Repository) workspace.ListInvitesUseCase {
	return &listInvitesUseCase{repo: repo}
}

func (uc *listInvitesUseCase) Execute(email string) ([]*workspace.Invite, error) {
	return uc.repo.ListInvitesByEmail(email)
}
