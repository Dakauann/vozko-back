package branch_usecase

import (
	"strings"

	"vozko/domain/branch"
)

type getBranchUseCase struct {
	repo branch.Repository
}

func NewGetUseCase(repo branch.Repository) branch.GetUseCase {
	return &getBranchUseCase{repo: repo}
}

func (uc *getBranchUseCase) Execute(id string) (*branch.Branch, error) {
	return uc.repo.FindByID(strings.TrimSpace(id))
}

type listByWorkspaceUseCase struct {
	repo branch.Repository
}

func NewListByWorkspaceUseCase(repo branch.Repository) branch.ListByWorkspaceUseCase {
	return &listByWorkspaceUseCase{repo: repo}
}

func (uc *listByWorkspaceUseCase) Execute(workspaceID string, page, pageSize int) ([]*branch.Branch, int64, error) {
	return uc.repo.FindByWorkspace(strings.TrimSpace(workspaceID), page, pageSize)
}

type listByUserUseCase struct {
	repo branch.Repository
}

func NewListByUserUseCase(repo branch.Repository) branch.ListByUserUseCase {
	return &listByUserUseCase{repo: repo}
}

func (uc *listByUserUseCase) Execute(workspaceID, userID string) ([]*branch.Branch, error) {
	return uc.repo.FindByUser(strings.TrimSpace(workspaceID), strings.TrimSpace(userID))
}
