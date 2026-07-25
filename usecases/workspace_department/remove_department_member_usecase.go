package workspace_department_usecase

import (
	wd "vozko/domain/workspace/workspace_department"
)

type removeMemberUseCase struct {
	repo wd.Repository
}

func NewRemoveMemberUseCase(repo wd.Repository) wd.RemoveMemberUseCase {
	return &removeMemberUseCase{repo: repo}
}

func (uc *removeMemberUseCase) Execute(departmentID, memberID string) error {
	return uc.repo.RemoveMember(departmentID, memberID)
}
