package workflow_usecase

import (
	"errors"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type getRunUseCase struct {
	runRepo    workflow.WorkflowRunRepository
	logRepo    workflow.WorkflowRunLogRepository
	visibility conversation.Visibility
}

func NewGetRunUseCase(
	runRepo workflow.WorkflowRunRepository,
	logRepo workflow.WorkflowRunLogRepository,
	visibility conversation.Visibility,
) workflow.GetRunUseCase {
	return &getRunUseCase{runRepo: runRepo, logRepo: logRepo, visibility: visibility}
}

func (uc *getRunUseCase) Execute(viewer shared.Person, workspaceID, runID string) (*workflow.RunDetail, error) {
	run, err := findVisibleRun(uc.runRepo, uc.visibility, viewer, workspaceID, runID)
	if err != nil {
		return nil, err
	}

	logs, err := uc.logRepo.FindByRunID(runID)
	if err != nil {
		return nil, err
	}

	return &workflow.RunDetail{Run: redactedRun(run), Logs: redactedRunLogs(logs)}, nil
}

func (uc *getRunUseCase) ExecutePage(viewer shared.Person, workspaceID, runID string, window workflow.StepWindow) (*workflow.RunDetail, error) {
	run, err := findVisibleRun(uc.runRepo, uc.visibility, viewer, workspaceID, runID)
	if err != nil {
		return nil, err
	}
	window = window.Normalized()
	total, err := uc.logRepo.CountByRun(run.ID)
	if err != nil {
		return nil, err
	}
	logs, err := uc.logRepo.PageByRunID(run.ID, window.Offset, window.Limit)
	if err != nil {
		return nil, err
	}
	return &workflow.RunDetail{
		Run:   redactedRun(run),
		Logs:  redactedRunLogs(logs),
		Steps: &workflow.RunStepsPage{Offset: window.Offset, Limit: window.Limit, Total: total, HasMore: int64(window.Offset+len(logs)) < total},
	}, nil
}

var _ workflow.RunDetailPages = (*getRunUseCase)(nil)

var ErrRunStepsUnpickable = errors.New("workflow: the step store cannot read single steps of a run")

func (uc *getRunUseCase) PickSteps(viewer shared.Person, workspaceID, runID string, pick workflow.StepPick) (*workflow.RunDetail, error) {
	run, err := findVisibleRun(uc.runRepo, uc.visibility, viewer, workspaceID, runID)
	if err != nil {
		return nil, err
	}
	picker, ok := uc.logRepo.(workflow.RunStepPicker)
	if !ok {
		return nil, ErrRunStepsUnpickable
	}
	if len(run.TriggerData) > 0 {
		pick.Trigger = false
	}
	logs, err := picker.PickSteps(run.ID, pick)
	if err != nil {
		return nil, err
	}
	return &workflow.RunDetail{Run: redactedRun(run), Logs: redactedRunLogs(logs)}, nil
}

func findWorkspaceRun(runRepo workflow.WorkflowRunRepository, workspaceID, runID string) (*workflow.WorkflowRun, error) {
	if workspaceID == "" || runID == "" {
		return nil, workflow.ErrRunNotFound
	}
	run, err := runRepo.FindByID(runID)
	if err != nil {
		return nil, err
	}
	if run == nil || run.WorkspaceID != workspaceID {
		return nil, workflow.ErrRunNotFound
	}
	return run, nil
}
