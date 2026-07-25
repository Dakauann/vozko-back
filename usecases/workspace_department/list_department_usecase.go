package workspace_department_usecase

import (
	wd "vozko/domain/workspace/workspace_department"
)

type listDepartmentsUseCase struct {
	repo wd.Repository
}

func NewListDepartmentsUseCase(repo wd.Repository) wd.ListDepartmentsUseCase {
	return &listDepartmentsUseCase{repo: repo}
}

func (uc *listDepartmentsUseCase) Execute(workspaceID string) ([]wd.Department, error) {
	return uc.repo.ListDepartments(workspaceID)
}
