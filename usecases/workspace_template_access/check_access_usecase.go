package workspace_template_access_usecase

import "vozko/domain/workspace_template_access"

type checkAccessUseCase struct {
	repo workspace_template_access.Repository
}

func NewCheckAccessUseCase(repo workspace_template_access.Repository) workspace_template_access.CheckAccessUseCase {
	return &checkAccessUseCase{repo: repo}
}

func (uc *checkAccessUseCase) Execute(workspaceID, templateID string) (bool, error) {
	return uc.repo.HasAccess(workspaceID, templateID)
}
