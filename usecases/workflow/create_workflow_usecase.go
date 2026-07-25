package workflow_usecase

import (
	"context"

	"github.com/google/uuid"

	"vozko/domain/workflow"
	workspace_department "vozko/domain/workspace/workspace_department"
)

type createWorkflowUseCase struct {
	repo               workflow.WorkflowRepository
	departmentResolver workspace_department.CreationDepartmentResolver
}

func NewCreateWorkflowUseCase(
	repo workflow.WorkflowRepository,
	departmentResolver ...workspace_department.CreationDepartmentResolver,
) workflow.CreateWorkflowUseCase {
	var resolver workspace_department.CreationDepartmentResolver
	if len(departmentResolver) > 0 {
		resolver = departmentResolver[0]
	}
	return &createWorkflowUseCase{repo: repo, departmentResolver: resolver}
}

func (uc *createWorkflowUseCase) Execute(ctx context.Context, w *workflow.Workflow) (*workflow.Workflow, error) {
	if w == nil {
		return nil, workflow.ErrNameRequired
	}

	w.Normalize()
	if err := w.Validate(); err != nil {
		return nil, err
	}

	if uc.departmentResolver != nil {
		departmentID, err := uc.departmentResolver.Resolve(ctx, w.WorkspaceID)
		if err != nil {
			return nil, err
		}
		w.DepartmentID = departmentID
	}

	if w.ID == "" {
		w.ID = uuid.New().String()
	}

	w.Status = workflow.WorkflowStatusDraft
	w.Version = 1

	if err := uc.repo.Create(w); err != nil {
		return nil, err
	}

	return uc.repo.FindByID(w.ID)
}
