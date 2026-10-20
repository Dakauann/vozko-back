package workflow_usecase

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"vozko/domain/cache"
	"vozko/domain/workflow"
)

const maxConcurrentRunsPerWorkspace = 50

func workspaceConcurrencyKey(workspaceID string) string {
	return fmt.Sprintf("wf:ws_concurrency:%s", workspaceID)
}

func tryAcquireWorkspaceSlot(state cache.SharedState, workspaceID string) bool {
	if state == nil {
		return true
	}
	ok, err := state.TryIncr(workspaceConcurrencyKey(workspaceID), maxConcurrentRunsPerWorkspace)
	if err != nil {
		slog.Warn("workflow.run_start.concurrency_check_failed_open", slog.String("workspace_id", workspaceID), slog.String("error", err.Error()))
		return true
	}
	return ok
}

func releaseWorkspaceSlot(state cache.SharedState, workspaceID string) {
	if state == nil {
		return
	}
	if _, err := state.Decr(workspaceConcurrencyKey(workspaceID)); err != nil {
		slog.Warn("workflow.run_start.concurrency_release_failed", slog.String("workspace_id", workspaceID), slog.String("error", err.Error()))
	}
}

func newTriggeredRun(w *workflow.Workflow, trigger *workflow.Node, event workflow.TriggerEvent) *workflow.WorkflowRun {
	state := workflow.NewRunState()
	for k, v := range event.Data {
		state.Set(k, v)
	}
	now := time.Now().UTC()
	return &workflow.WorkflowRun{
		ID:              triggeredRunID(w.ID, event),
		WorkflowID:      w.ID,
		WorkspaceID:     event.WorkspaceID,
		EntryID:         event.EntryID,
		EntryType:       event.EntryType,
		Status:          workflow.RunStatusRunning,
		TriggerNodeID:   trigger.ID,
		CurrentNodeID:   trigger.ID,
		State:           state,
		StartedAt:       now,
		UpdatedAt:       now,
		TriggerType:     event.TriggerType,
		StartedBy:       event.Starter(),
		WorkflowVersion: graphVersionOf(w),
		TriggerData:     workflow.TriggerDataOf(state),
	}
}

func graphVersionOf(w *workflow.Workflow) *int {
	if w == nil || w.GraphVersion == nil {
		return nil
	}
	version := *w.GraphVersion
	return &version
}

func pinnedWorkflow(repo workflow.PublishedReader, run *workflow.WorkflowRun, loaded *workflow.Workflow) (*workflow.Workflow, error) {
	if loaded != nil && loaded.ID == run.WorkflowID && run.WorkflowVersion != nil && loaded.GraphVersion != nil && *run.WorkflowVersion == *loaded.GraphVersion {
		return loaded, nil
	}
	return workflow.LoadForRun(repo, run)
}

var occurrenceRunSpace = uuid.MustParse("6f1c2b9e-4d3a-5e7f-8a1b-2c3d4e5f6a7b")

func triggeredRunID(workflowID string, event workflow.TriggerEvent) string {
	if key := event.OccurrenceKey(); key != "" {
		return uuid.NewSHA1(occurrenceRunSpace, []byte(workflowID+"/"+key)).String()
	}
	return uuid.New().String()
}

func executeLocked(engine *RunEngine, run *workflow.WorkflowRun, w *workflow.Workflow) {
	if !engine.TryLockRun(run.ID) {
		slog.Info("workflow.run_start.already_locked", runLogAttrs(run, run.CurrentNodeID)...)
		return
	}
	defer engine.UnlockRun(run.ID)
	if err := engine.Execute(run, w); err != nil {
		slog.Error("workflow.run_start.engine_error", append(runLogAttrs(run, run.CurrentNodeID), slog.String("error", outbound.String(err.Error())))...)
	}
}

type RunLauncher interface {
	Launch(run *workflow.WorkflowRun, w *workflow.Workflow)
}

type engineRunLauncher struct {
	engine      *RunEngine
	sharedState cache.SharedState
	dispatch    func(func())
}

func NewEngineRunLauncher(engine *RunEngine, sharedState cache.SharedState) RunLauncher {
	return &engineRunLauncher{
		engine:      engine,
		sharedState: sharedState,
		dispatch:    func(fn func()) { go fn() },
	}
}

func (l *engineRunLauncher) Launch(run *workflow.WorkflowRun, w *workflow.Workflow) {
	l.dispatch(func() {
		defer releaseWorkspaceSlot(l.sharedState, run.WorkspaceID)
		executeLocked(l.engine, run, w)
	})
}
