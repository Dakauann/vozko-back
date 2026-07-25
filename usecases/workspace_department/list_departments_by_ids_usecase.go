package workspace_department_usecase

import (
	wd "vozko/domain/workspace/workspace_department"
)

type listDepartmentsByIDsUseCase struct {
	repo wd.Repository
}

func NewListDepartmentsByIDsUseCase(repo wd.Repository) wd.ListDepartmentsByIDsUseCase {
	return &listDepartmentsByIDsUseCase{repo: repo}
}

func (uc *listDepartmentsByIDsUseCase) Execute(ids []string) ([]wd.Department, error) {
	return uc.repo.ListDepartmentsByIDs(ids)
}
