package workspace_phone_access_usecase

import "vozko/domain/workspace_phone_access"

type checkAccessUseCase struct {
	repo workspace_phone_access.Repository
}

func NewCheckAccessUseCase(repo workspace_phone_access.Repository) workspace_phone_access.CheckAccessUseCase {
	return &checkAccessUseCase{repo: repo}
}

func (uc *checkAccessUseCase) Execute(workspaceID, phoneID string) (bool, error) {
	return uc.repo.HasAccess(workspaceID, phoneID)
}
