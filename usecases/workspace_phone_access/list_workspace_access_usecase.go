package workspace_phone_access_usecase

import (
	"vozko/domain/shared"
	"vozko/domain/workspace_phone_access"
)

type listWorkspaceAccessUseCase struct {
	repo workspace_phone_access.Repository
}

func NewListWorkspaceAccessUseCase(repo workspace_phone_access.Repository) workspace_phone_access.ListWorkspaceAccessUseCase {
	return &listWorkspaceAccessUseCase{repo: repo}
}

func (uc *listWorkspaceAccessUseCase) Execute(input workspace_phone_access.ListWorkspaceAccessInput) (*shared.PaginatedResult[*workspace_phone_access.WorkspacePhoneAccess], error) {
	return uc.repo.ListByWorkspace(input)
}
