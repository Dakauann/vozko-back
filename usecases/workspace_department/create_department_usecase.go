package workspace_department_usecase

import (
	"strings"

	"github.com/google/uuid"

	wd "vozko/domain/workspace/workspace_department"
)

type createDepartmentUseCase struct {
	repo wd.Repository
}

func NewCreateDepartmentUseCase(repo wd.Repository) wd.CreateDepartmentUseCase {
	return &createDepartmentUseCase{repo: repo}
}

func (uc *createDepartmentUseCase) Execute(input wd.CreateDepartmentInput) (*wd.Department, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, wd.ErrDepartmentNameRequired
	}

	dept := &wd.Department{
		ID:          uuid.New().String(),
		WorkspaceID: input.WorkspaceID,
		Name:        name,
		Description: strings.TrimSpace(input.Description),
	}

	if err := uc.repo.CreateDepartment(dept); err != nil {
		return nil, err
	}
	return dept, nil
}
