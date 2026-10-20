package workflow_usecase

import (
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type listRunsUseCase struct {
	runRepo    workflow.WorkflowRunRepository
	visibility conversation.Visibility
}

func NewListRunsUseCase(runRepo workflow.WorkflowRunRepository, visibility conversation.Visibility) workflow.ListRunsUseCase {
	return &listRunsUseCase{runRepo: runRepo, visibility: visibility}
}

func (uc *listRunsUseCase) Execute(viewer shared.Person, input workflow.ListRunsInput) (*shared.PaginatedResult[*workflow.WorkflowRun], error) {
	input.Visibility = conversationScope(uc.visibility, viewer, input.WorkspaceID)
	result, err := uc.runRepo.List(input)
	if err != nil || result == nil {
		return result, err
	}
	page := *result
	page.Items = make([]*workflow.WorkflowRun, len(result.Items))
	for i, run := range result.Items {
		page.Items[i] = redactedRun(run)
	}
	return &page, nil
}
