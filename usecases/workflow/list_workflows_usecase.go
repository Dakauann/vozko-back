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
	return uc.repo.List(input)
}
