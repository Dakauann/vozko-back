package workspace_usecase

import (
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

type listMembersPaginatedUseCase struct {
	repo workspace.Repository
}

func NewListMembersPaginatedUseCase(repo workspace.Repository) workspace.ListMembersPaginatedUseCase {
	return &listMembersPaginatedUseCase{repo: repo}
}

func (uc *listMembersPaginatedUseCase) Execute(workspaceID string, page, pageSize int) ([]*workspace.Member, int64, error) {
	if pageSize <= 0 {
		pageSize = shared.DefaultPageSize
	}
	if page <= 0 {
		page = 1
	}
	return uc.repo.ListMembersPaginated(workspaceID, page, pageSize)
}
