package workflow_usecase

import (
	"time"

	"github.com/google/uuid"

	"vozko/domain/workflow"
)

type startRunUseCase struct {
	workflowRepo workflow.WorkflowRepository
	runRepo      workflow.WorkflowRunRepository
}

func NewStartRunUseCase(
	workflowRepo workflow.WorkflowRepository,
	runRepo workflow.WorkflowRunRepository,
) workflow.StartRunUseCase {
	return &startRunUseCase{
		workflowRepo: workflowRepo,
		runRepo:      runRepo,
	}
}

func (uc *startRunUseCase) Execute(input workflow.StartRunInput) (*workflow.WorkflowRun, error) {
	w, err := uc.workflowRepo.FindByID(input.WorkflowID)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, workflow.ErrWorkflowNotFound
	}
	if w.Status != workflow.WorkflowStatusActive {
		return nil, workflow.ErrWorkflowNotActive
	}

	trigger := w.Graph.TriggerNode()
	if trigger == nil {
		return nil, workflow.ErrGraphNoTrigger
	}

	existing, err := uc.runRepo.FindActiveByEntryAndTrigger(input.WorkflowID, input.EntryID, trigger.ID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, workflow.ErrRunAlreadyExists
	}

	state := workflow.NewRunState()
	for k, v := range input.Variables {
		state.Set(k, v)
	}

	now := time.Now().UTC()
	run := &workflow.WorkflowRun{
		ID:            uuid.New().String(),
		WorkflowID:    input.WorkflowID,
		WorkspaceID:   input.WorkspaceID,
		EntryID:       input.EntryID,
		EntryType:     input.EntryType,
		Status:        workflow.RunStatusRunning,
		TriggerNodeID: trigger.ID,
		CurrentNodeID: trigger.ID,
		State:         state,
		StartedAt:     now,
		UpdatedAt:     now,
	}

	if err := run.Validate(); err != nil {
		return nil, err
	}

	if err := uc.runRepo.Create(run); err != nil {
		return nil, err
	}

	return run, nil
}
