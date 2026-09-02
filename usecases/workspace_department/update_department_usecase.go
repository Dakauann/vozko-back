package workspace_department_usecase

import (
	"strings"

	wd "vozko/domain/workspace/workspace_department"
)

type updateDepartmentUseCase struct {
	repo wd.Repository
}

func NewUpdateDepartmentUseCase(repo wd.Repository) wd.UpdateDepartmentUseCase {
	return &updateDepartmentUseCase{repo: repo}
}

func (uc *updateDepartmentUseCase) Execute(id string, input wd.UpdateDepartmentInput) (*wd.Department, error) {
	dept, err := uc.repo.GetDepartmentByID(id)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, wd.ErrDepartmentNameRequired
		}
		dept.Name = name
	}
	if input.Description != nil {
		dept.Description = strings.TrimSpace(*input.Description)
	}

	// Same posture as the workspace-level policy: an invalid schedule is
	// refused, never quietly rewritten, because a working-hours window nobody
	// can see is a window nobody can debug. Clearing it hands the department
	// back to the workspace schedule.
	switch {
	case input.ClearWorkingHours:
		dept.WorkingHours = nil
	case input.WorkingHours != nil:
		if err := input.WorkingHours.Validate(); err != nil {
			return nil, err
		}
		dept.WorkingHours = input.WorkingHours.Normalized()
	}

	if err := uc.repo.UpdateDepartment(dept); err != nil {
		return nil, err
	}
	return dept, nil
}
