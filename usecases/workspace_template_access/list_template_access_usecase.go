package workspace_template_access_usecase

import (
	"vozko/domain/shared"
	"vozko/domain/workspace_template_access"
)

type listTemplateAccessUseCase struct {
	repo workspace_template_access.Repository
}

func NewListTemplateAccessUseCase(repo workspace_template_access.Repository) workspace_template_access.ListTemplateAccessUseCase {
	return &listTemplateAccessUseCase{repo: repo}
}

func (uc *listTemplateAccessUseCase) Execute(input workspace_template_access.ListTemplateAccessInput) (*shared.PaginatedResult[*workspace_template_access.WorkspaceTemplateAccess], error) {
	return uc.repo.ListByTemplate(input)
}
