package workflow_usecase

import (
	"vozko/domain/workflow"
)

type deleteWorkflowUseCase struct {
	repo    workflow.WorkflowRepository
	runRepo workflow.WorkflowRunRepository
}

func NewDeleteWorkflowUseCase(repo workflow.WorkflowRepository, runRepo workflow.WorkflowRunRepository) workflow.DeleteWorkflowUseCase {
	return &deleteWorkflowUseCase{repo: repo, runRepo: runRepo}
}

func (uc *deleteWorkflowUseCase) Execute(workflowID string) error {
	if workflowID == "" {
		return workflow.ErrWorkflowNotFound
	}

	existing, err := uc.repo.FindByID(workflowID)
	if err != nil {
		return err
	}
	if existing == nil {
		return workflow.ErrWorkflowNotFound
	}

	if _, err := uc.runRepo.CancelByWorkflow(workflowID); err != nil {
		return err
	}

	return uc.repo.Delete(workflowID)
}
