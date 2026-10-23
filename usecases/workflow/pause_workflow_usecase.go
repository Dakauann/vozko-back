package workflow_usecase

import (
	"fmt"
	"vozko/domain/workflow"
)

type pauseWorkflowUseCase struct {
	repo workflow.WorkflowRepository
	runs workflow.WorkflowRunRepository
}

func NewPauseWorkflowUseCase(repo workflow.WorkflowRepository, runs workflow.WorkflowRunRepository) workflow.PauseWorkflowUseCase {
	if repo == nil || runs == nil {
		panic("workflow: pausing requires workflows and run cancellation")
	}
	return &pauseWorkflowUseCase{repo: repo, runs: runs}
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

	if w.Status != workflow.WorkflowStatusActive && w.Status != workflow.WorkflowStatusPaused {
		return nil, workflow.ErrWorkflowNotActive
	}

	if err := uc.repo.UpdateStatus(w.ID, workflow.WorkflowStatusPaused); err != nil {
		return nil, err
	}
	if _, err := uc.runs.CancelByWorkflow(w.ID); err != nil {
		return nil, fmt.Errorf("workflow paused but its executions could not be stopped: %w", err)
	}

	return findWithPublication(uc.repo, workflowID)
}
