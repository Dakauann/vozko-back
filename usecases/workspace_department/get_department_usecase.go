package workspace_department_usecase

import (
	wd "vozko/domain/workspace/workspace_department"
)

type getDepartmentUseCase struct {
	repo wd.Repository
}

func NewGetDepartmentUseCase(repo wd.Repository) wd.GetDepartmentUseCase {
	return &getDepartmentUseCase{repo: repo}
}

func (uc *getDepartmentUseCase) Execute(id string) (*wd.Department, error) {
	return uc.repo.GetDepartmentByID(id)
}
