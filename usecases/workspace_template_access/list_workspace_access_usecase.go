package workspace_template_access_usecase

import (
	"vozko/domain/shared"
	"vozko/domain/workspace_template_access"
)

type listWorkspaceAccessUseCase struct {
	repo workspace_template_access.Repository
}

func NewListWorkspaceAccessUseCase(repo workspace_template_access.Repository) workspace_template_access.ListWorkspaceAccessUseCase {
	return &listWorkspaceAccessUseCase{repo: repo}
}

func (uc *listWorkspaceAccessUseCase) Execute(input workspace_template_access.ListWorkspaceAccessInput) (*shared.PaginatedResult[*workspace_template_access.WorkspaceTemplateAccess], error) {
	return uc.repo.ListByWorkspace(input)
}
