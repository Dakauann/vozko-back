package workflow_usecase

import (
	"vozko/domain/workflow"
)

type cancelRunUseCase struct {
	runRepo workflow.WorkflowRunRepository
}

func NewCancelRunUseCase(runRepo workflow.WorkflowRunRepository) workflow.CancelRunUseCase {
	return &cancelRunUseCase{runRepo: runRepo}
}

func (uc *cancelRunUseCase) Execute(runID string) error {
	if runID == "" {
		return workflow.ErrRunNotFound
	}

	run, err := uc.runRepo.FindByID(runID)
	if err != nil {
		return err
	}
	if run == nil {
		return workflow.ErrRunNotFound
	}

	if run.Status.IsTerminal() {
		return workflow.ErrRunTerminal
	}

	run.SetCancelled()
	return uc.runRepo.Update(run)
}
