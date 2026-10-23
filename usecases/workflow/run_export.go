package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

const runExportPageSize = 50

var ErrRunExportStepsOutOfOrder = errors.New("workflow: the exported steps did not come in the order of their runs")

type RunExportWorkflows interface {
	Get(scope workflow.Scope, workflowID string) (*workflow.Workflow, error)
}

type RunExportSink interface {
	Start(workflowName string) error
	Write(doc workflow.RunExport) error
	Flush() error
}

type RunExportUseCase struct {
	detail     workflow.GetRunUseCase
	workflows  RunExportWorkflows
	runs       workflow.RunExportSource
	steps      workflow.RunStepsReader
	visibility conversation.Visibility
	now        func() time.Time
}

func NewRunExportUseCase(detail workflow.GetRunUseCase, workflows RunExportWorkflows, runs workflow.RunExportSource, steps workflow.RunStepsReader, visibility conversation.Visibility) *RunExportUseCase {
	return &RunExportUseCase{
		detail:     detail,
		workflows:  workflows,
		runs:       runs,
		steps:      steps,
		visibility: visibility,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

func (uc *RunExportUseCase) Run(viewer shared.Person, scope workflow.Scope, runID string) (*workflow.RunExport, error) {
	detail, err := uc.detail.Execute(viewer, scope.WorkspaceID, runID)
	if err != nil {
		return nil, err
	}
	wf, err := uc.workflows.Get(scope, detail.Run.WorkflowID)
	if err != nil {
		return nil, err
	}
	doc := workflow.ExportOfRun(wf.Name, *detail, uc.now())
	return &doc, nil
}

func (uc *RunExportUseCase) Stream(ctx context.Context, viewer shared.Person, scope workflow.Scope, workflowID string, filter workflow.RunExportFilter, sink RunExportSink) error {
	wf, err := uc.workflows.Get(scope, workflowID)
	if err != nil {
		return err
	}
	if err := filter.Validate(); err != nil {
		return err
	}
	filter.Runs.WorkspaceID, filter.Runs.WorkflowID = scope.WorkspaceID, wf.ID
	filter.Runs.Visibility = conversationScope(uc.visibility, viewer, scope.WorkspaceID)

	total, err := uc.runs.CountForExportUpTo(ctx, filter, workflow.MaxRunsPerExport+1)
	if err != nil {
		return err
	}
	if total > workflow.MaxRunsPerExport {
		return fmt.Errorf("%w: more than %d runs", workflow.ErrRunExportTooLarge, workflow.MaxRunsPerExport)
	}
	if err := sink.Start(wf.Name); err != nil {
		return err
	}

	exportedAt := uc.now()
	var after *workflow.RunCursor
	written := 0
	for written < workflow.MaxRunsPerExport {
		if err := ctx.Err(); err != nil {
			return err
		}
		limit := min(runExportPageSize, workflow.MaxRunsPerExport-written)
		page, err := uc.runs.ExportPage(ctx, filter, after, limit)
		if err != nil {
			return err
		}
		if err := uc.writePage(ctx, wf.Name, page, exportedAt, sink); err != nil {
			return err
		}
		if err := sink.Flush(); err != nil {
			return err
		}
		written += len(page)
		if len(page) < limit {
			break
		}
		after = workflow.CursorAfter(page[len(page)-1])
	}
	slog.Info("workflow.runs_exported", slog.String("workspace_id", scope.WorkspaceID), slog.String("workflow_id", wf.ID), slog.Int("runs", written))
	return nil
}

func (uc *RunExportUseCase) writePage(ctx context.Context, workflowName string, page []*workflow.WorkflowRun, exportedAt time.Time, sink RunExportSink) error {
	if len(page) == 0 {
		return nil
	}
	ids := make([]string, len(page))
	for i, run := range page {
		ids[i] = run.ID
	}
	next := 0
	write := func(run *workflow.WorkflowRun, steps []*workflow.WorkflowRunLog) error {
		return sink.Write(workflow.ExportOfRun(workflowName, workflow.RunDetail{Run: redactedRun(run), Logs: redactedRunLogs(steps)}, exportedAt))
	}
	err := uc.steps.EachRunSteps(ctx, ids, func(runID string, steps []*workflow.WorkflowRunLog) error {
		for next < len(page) && page[next].ID != runID {
			if err := write(page[next], nil); err != nil {
				return err
			}
			next++
		}
		if next == len(page) {
			return fmt.Errorf("%w: run %s", ErrRunExportStepsOutOfOrder, runID)
		}
		next++
		return write(page[next-1], steps)
	})
	if err != nil {
		return err
	}
	for ; next < len(page); next++ {
		if err := write(page[next], nil); err != nil {
			return err
		}
	}
	return nil
}
