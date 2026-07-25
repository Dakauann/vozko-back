package workflow_usecase

import (
	"vozko/domain/workflow"
)

type getWorkflowUseCase struct {
	repo workflow.WorkflowRepository
}

func NewGetWorkflowUseCase(repo workflow.WorkflowRepository) workflow.GetWorkflowUseCase {
	return &getWorkflowUseCase{repo: repo}
}

func (uc *getWorkflowUseCase) Execute(workflowID string) (*workflow.Workflow, error) {
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
	return w, nil
}
