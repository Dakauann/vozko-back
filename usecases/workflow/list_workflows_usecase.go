package workflow_usecase

import (
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type listWorkflowsUseCase struct {
	repo workflow.WorkflowRepository
}

func NewListWorkflowsUseCase(repo workflow.WorkflowRepository) workflow.ListWorkflowsUseCase {
	return &listWorkflowsUseCase{repo: repo}
}

func (uc *listWorkflowsUseCase) Execute(input workflow.ListWorkflowsInput) (*shared.PaginatedResult[*workflow.Workflow], error) {
	page, err := uc.repo.List(input)
	if err != nil || page == nil {
		return page, err
	}
	for i, w := range page.Items {
		if w != nil {
			w.RefreshPublication()
			page.Items[i] = withoutProductionPins(w)
		}
	}
	return page, nil
}
