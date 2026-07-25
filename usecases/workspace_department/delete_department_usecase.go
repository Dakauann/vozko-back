package workspace_department_usecase

import (
	wd "vozko/domain/workspace/workspace_department"
)

type deleteDepartmentUseCase struct {
	repo wd.Repository
}

func NewDeleteDepartmentUseCase(repo wd.Repository) wd.DeleteDepartmentUseCase {
	return &deleteDepartmentUseCase{repo: repo}
}

func (uc *deleteDepartmentUseCase) Execute(id string) error {
	return uc.repo.DeleteDepartment(id)
}
