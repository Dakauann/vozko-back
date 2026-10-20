package workflow_usecase

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/metrics"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

var errCancelRunIncomplete = errors.New("workflow: cancelling a run needs the run store, the status-only cancel and the step log")

type cancelRunUseCase struct {
	runs       workflow.WorkflowRunRepository
	cancels    workflow.RunCanceller
	logs       workflow.WorkflowRunLogRepository
	metrics    metrics.WorkflowMetricsRecorder
	visibility conversation.Visibility
}

func NewCancelRunUseCase(runs workflow.WorkflowRunRepository, cancels workflow.RunCanceller, logs workflow.WorkflowRunLogRepository, recorder metrics.WorkflowMetricsRecorder, visibility conversation.Visibility) workflow.CancelRunUseCase {
	return &cancelRunUseCase{runs: runs, cancels: cancels, logs: logs, metrics: recorder, visibility: visibility}
}

func (uc *cancelRunUseCase) Execute(viewer shared.Person, workspaceID, runID string) error {
	if uc.cancels == nil || uc.logs == nil {
		return errCancelRunIncomplete
	}
	run, err := findVisibleRun(uc.runs, uc.visibility, viewer, workspaceID, runID)
	if err != nil {
		return err
	}
	if run.Status.IsTerminal() {
		return workflow.ErrRunTerminal
	}
	cancelled, err := uc.cancels.CancelRun(run.ID, workflow.CancelReasonOperator)
	if err != nil {
		return err
	}
	if !cancelled {
		return fmt.Errorf("run %s: %w", run.ID, workflow.ErrRunTerminal)
	}
	if uc.metrics != nil {
		uc.metrics.IncWorkflowRunFinished(string(workflow.RunStatusCancelled))
	}
	traceCancelledStep(uc.logs, run.ID, run.CurrentNodeID, workflow.CancelReasonOperator, append(runLogAttrs(run, run.CurrentNodeID), slog.String("workflow_id", run.WorkflowID)))
	return nil
}

func traceCancelledStep(logs workflow.WorkflowRunLogRepository, runID, nodeID, reason string, attrs []any) {
	now := time.Now().UTC()
	row := stepRow(runID, step{
		nodeID: nodeID, status: workflow.LogStatusCancelled, attempt: 1, started: now, finished: now,
		err: workflow.Fail(workflow.ErrorKindCancelled, errors.New(reason)),
	})
	if err := logs.Create(storedStep(row)); err != nil {
		slog.Error("workflow.run.cancel_step_write_failed", append(attrs, slog.String("error", err.Error()))...)
		return
	}
	slog.Info("workflow.run.cancelled", append(attrs, slog.String("reason", reason))...)
}
