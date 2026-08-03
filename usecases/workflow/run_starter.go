package workflow_usecase

import (
	"fmt"
	"log"
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
		log.Printf("[workflow] run-start: concurrency check failed for workspace %s (fail-open): %v", workspaceID, err)
		return true
	}
	return ok
}

func releaseWorkspaceSlot(state cache.SharedState, workspaceID string) {
	if state == nil {
		return
	}
	if _, err := state.Decr(workspaceConcurrencyKey(workspaceID)); err != nil {
		log.Printf("[workflow] run-start: failed to decrement concurrency for workspace %s: %v", workspaceID, err)
	}
}

func newTriggeredRun(w *workflow.Workflow, trigger *workflow.Node, event workflow.TriggerEvent) *workflow.WorkflowRun {
	state := workflow.NewRunState()
	for k, v := range event.Data {
		state.Set(k, v)
	}
	now := time.Now().UTC()
	return &workflow.WorkflowRun{
		ID:            uuid.New().String(),
		WorkflowID:    w.ID,
		WorkspaceID:   event.WorkspaceID,
		EntryID:       event.EntryID,
		EntryType:     event.EntryType,
		Status:        workflow.RunStatusRunning,
		TriggerNodeID: trigger.ID,
		CurrentNodeID: trigger.ID,
		State:         state,
		StartedAt:     now,
		UpdatedAt:     now,
	}
}

func executeLocked(engine *RunEngine, run *workflow.WorkflowRun, w *workflow.Workflow) {
	if !engine.TryLockRun(run.ID) {
		log.Printf("[workflow] run-start: skipping run %s, already locked", run.ID)
		return
	}
	defer engine.UnlockRun(run.ID)
	if err := engine.Execute(run, w); err != nil {
		log.Printf("[workflow] run-start: engine error for run %s: %v", run.ID, err)
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
