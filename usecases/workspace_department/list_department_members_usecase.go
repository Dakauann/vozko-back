package workspace_department_usecase

import (
	wd "vozko/domain/workspace/workspace_department"
)

type listMembersUseCase struct {
	repo wd.Repository
}

func NewListMembersUseCase(repo wd.Repository) wd.ListMembersUseCase {
	return &listMembersUseCase{repo: repo}
}

func (uc *listMembersUseCase) Execute(departmentID string) ([]wd.DepartmentMember, error) {
	return uc.repo.ListMembers(departmentID)
}
