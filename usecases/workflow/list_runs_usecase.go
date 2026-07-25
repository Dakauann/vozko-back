package workflow_usecase

import (
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type listRunsUseCase struct {
	runRepo workflow.WorkflowRunRepository
}

func NewListRunsUseCase(runRepo workflow.WorkflowRunRepository) workflow.ListRunsUseCase {
	return &listRunsUseCase{runRepo: runRepo}
}

func (uc *listRunsUseCase) Execute(input workflow.ListRunsInput) (*shared.PaginatedResult[*workflow.WorkflowRun], error) {
	return uc.runRepo.List(input)
}
