package workspace_template_access_usecase

import "vozko/domain/workspace_template_access"

type revokeAccessUseCase struct {
	repo workspace_template_access.Repository
}

func NewRevokeAccessUseCase(repo workspace_template_access.Repository) workspace_template_access.RevokeAccessUseCase {
	return &revokeAccessUseCase{repo: repo}
}

func (uc *revokeAccessUseCase) Execute(input workspace_template_access.RevokeAccessInput) error {
	return uc.repo.DeleteByWorkspaceAndTemplate(input.WorkspaceID, input.TemplateID)
}
