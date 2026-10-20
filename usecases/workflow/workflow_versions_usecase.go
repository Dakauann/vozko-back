package workflow_usecase

import (
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

func findWithPublication(repo workflow.WorkflowRepository, workflowID string) (*workflow.Workflow, error) {
	w, err := repo.FindByID(workflowID)
	if err != nil || w == nil {
		return w, err
	}
	w.RefreshPublication()
	return w, nil
}

type getPublishedWorkflowUseCase struct {
	repo workflow.WorkflowRepository
}

func NewGetPublishedWorkflowUseCase(repo workflow.WorkflowRepository) workflow.GetWorkflowUseCase {
	return &getPublishedWorkflowUseCase{repo: repo}
}

func (uc *getPublishedWorkflowUseCase) Execute(workflowID string) (*workflow.Workflow, error) {
	if workflowID == "" {
		return nil, workflow.ErrWorkflowNotFound
	}
	w, err := workflow.LoadPublished(uc.repo, workflowID)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, workflow.ErrWorkflowNotFound
	}
	return w, nil
}

type workflowVersionsUseCase struct {
	repo workflow.WorkflowRepository
}

func NewWorkflowVersionsUseCase(repo workflow.WorkflowRepository) workflow.WorkflowVersionsUseCase {
	return &workflowVersionsUseCase{repo: repo}
}

func (uc *workflowVersionsUseCase) List(workflowID string, options shared.QueryOptions) (*shared.PaginatedResult[*workflow.WorkflowVersion], error) {
	if workflowID == "" {
		return nil, workflow.ErrWorkflowNotFound
	}
	return uc.repo.ListVersions(workflowID, options)
}

func (uc *workflowVersionsUseCase) Find(workflowID string, version int) (*workflow.WorkflowVersion, error) {
	if workflowID == "" {
		return nil, workflow.ErrWorkflowNotFound
	}
	if version < 1 {
		return nil, workflow.ErrInvalidVersion
	}
	return workflow.FindVersionOf(uc.repo, workflowID, version)
}
