package workflow_usecase

import (
	"context"

	"vozko/domain/workflow"
	workspace_department "vozko/domain/workspace/workspace_department"
)

type assignDepartmentUseCase struct {
	repo               workflow.WorkflowRepository
	departmentResolver workspace_department.CreationDepartmentResolver
}

func NewAssignDepartmentUseCase(
	repo workflow.WorkflowRepository,
	departmentResolver workspace_department.CreationDepartmentResolver,
) workflow.AssignDepartmentUseCase {
	return &assignDepartmentUseCase{
		repo:               repo,
		departmentResolver: departmentResolver,
	}
}

func (uc *assignDepartmentUseCase) Execute(ctx context.Context, workflowID string) (*workflow.Workflow, error) {
	if workflowID == "" {
		return nil, workflow.ErrWorkflowNotFound
	}

	current, err := uc.repo.FindByID(workflowID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, workflow.ErrWorkflowNotFound
	}

	departmentID, err := uc.departmentResolver.Resolve(ctx, current.WorkspaceID)
	if err != nil {
		return nil, err
	}

	current.DepartmentID = departmentID
	if err := uc.repo.Update(current); err != nil {
		return nil, err
	}

	return uc.repo.FindByID(workflowID)
}
