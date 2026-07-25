package workspace_phone_access_usecase

import "vozko/domain/workspace_phone_access"

type revokeAccessUseCase struct {
	repo workspace_phone_access.Repository
}

func NewRevokeAccessUseCase(repo workspace_phone_access.Repository) workspace_phone_access.RevokeAccessUseCase {
	return &revokeAccessUseCase{repo: repo}
}

func (uc *revokeAccessUseCase) Execute(input workspace_phone_access.RevokeAccessInput) error {
	return uc.repo.DeleteByWorkspaceAndPhone(input.WorkspaceID, input.PhoneID)
}
