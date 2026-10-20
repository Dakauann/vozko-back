package workflow_usecase

import (
	"vozko/domain/workflow"
)

type pauseWorkflowUseCase struct {
	repo workflow.WorkflowRepository
}

func NewPauseWorkflowUseCase(repo workflow.WorkflowRepository) workflow.PauseWorkflowUseCase {
	return &pauseWorkflowUseCase{repo: repo}
}

func (uc *pauseWorkflowUseCase) Execute(workflowID string) (*workflow.Workflow, error) {
	if workflowID == "" {
		return nil, workflow.ErrWorkflowNotFound
	}

	w, err := uc.repo.FindByID(workflowID)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, workflow.ErrWorkflowNotFound
	}

	if w.Status != workflow.WorkflowStatusActive {
		return nil, workflow.ErrWorkflowNotActive
	}

	if err := uc.repo.UpdateStatus(w.ID, workflow.WorkflowStatusPaused); err != nil {
		return nil, err
	}

	return findWithPublication(uc.repo, workflowID)
}
