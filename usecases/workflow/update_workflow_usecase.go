package workflow_usecase

import (
	"context"

	"vozko/domain/workflow"
)

type updateWorkflowUseCase struct {
	repo         workflow.WorkflowRepository
	modelCatalog ModelCatalog
}

func NewUpdateWorkflowUseCase(repo workflow.WorkflowRepository) workflow.UpdateWorkflowUseCase {
	return &updateWorkflowUseCase{repo: repo}
}

func (uc *updateWorkflowUseCase) SetModelCatalog(catalog ModelCatalog) {
	uc.modelCatalog = catalog
}

func (uc *updateWorkflowUseCase) Execute(ctx context.Context, workflowID string, input *workflow.Workflow, revision int) (*workflow.Workflow, error) {
	if workflowID == "" {
		return nil, workflow.ErrWorkflowNotFound
	}
	if input == nil {
		return nil, workflow.ErrNameRequired
	}
	if err := workflow.RequireDraftRevision(revision); err != nil {
		return nil, err
	}

	existing, err := uc.repo.FindByID(workflowID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, workflow.ErrWorkflowNotFound
	}

	existing.Name = input.Name
	existing.Description = input.Description

	if input.Type != "" {
		existing.Type = input.Type
	}
	existing.TriggerType = ""
	existing.TriggerConfig = nil
	existing.Graph = workflow.RestoreWithheldPins(input.Graph, existing.Graph)

	existing.Normalize()
	if err := existing.Validate(); err != nil {
		return nil, err
	}
	if err := checkGraphReasoning(ctx, uc.modelCatalog, &existing.Graph); err != nil {
		return nil, err
	}

	if _, err := uc.repo.SaveDraft(existing, revision); err != nil {
		return nil, err
	}

	return findWithPublication(uc.repo, workflowID)
}
