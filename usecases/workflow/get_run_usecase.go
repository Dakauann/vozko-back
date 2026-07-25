package workflow_usecase

import (
	"vozko/domain/workflow"
)

type getRunUseCase struct {
	runRepo workflow.WorkflowRunRepository
	logRepo workflow.WorkflowRunLogRepository
}

func NewGetRunUseCase(
	runRepo workflow.WorkflowRunRepository,
	logRepo workflow.WorkflowRunLogRepository,
) workflow.GetRunUseCase {
	return &getRunUseCase{runRepo: runRepo, logRepo: logRepo}
}

func (uc *getRunUseCase) Execute(runID string) (*workflow.RunDetail, error) {
	if runID == "" {
		return nil, workflow.ErrRunNotFound
	}

	run, err := uc.runRepo.FindByID(runID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, workflow.ErrRunNotFound
	}

	logs, err := uc.logRepo.FindByRunID(runID)
	if err != nil {
		return nil, err
	}

	return &workflow.RunDetail{Run: run, Logs: logs}, nil
}
