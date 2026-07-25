package workflow_usecase

import (
	"vozko/domain/workflow"
)

type updateWorkflowUseCase struct {
	repo workflow.WorkflowRepository
}

func NewUpdateWorkflowUseCase(repo workflow.WorkflowRepository) workflow.UpdateWorkflowUseCase {
	return &updateWorkflowUseCase{repo: repo}
}

func (uc *updateWorkflowUseCase) Execute(workflowID string, input *workflow.Workflow) (*workflow.Workflow, error) {
	if workflowID == "" {
		return nil, workflow.ErrWorkflowNotFound
	}
	if input == nil {
		return nil, workflow.ErrNameRequired
	}

	existing, err := uc.repo.FindByID(workflowID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, workflow.ErrWorkflowNotFound
	}

	if existing.Status != workflow.WorkflowStatusDraft {
		existing.Status = workflow.WorkflowStatusDraft
	}

	existing.Name = input.Name
	existing.Description = input.Description

	if input.Type != "" {
		existing.Type = input.Type
	}
	existing.TriggerType = ""
	existing.TriggerConfig = nil
	existing.Graph = input.Graph

	existing.Normalize()
	if err := existing.Validate(); err != nil {
		return nil, err
	}

	existing.Version++

	if err := uc.repo.Update(existing); err != nil {
		return nil, err
	}

	return uc.repo.FindByID(workflowID)
}
