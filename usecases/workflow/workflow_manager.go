package workflow_usecase

import (
	"log"
	"time"

	"vozko/domain/workflow"
)

type workflowManager struct {
	workflowRepo workflow.WorkflowRepository
	runRepo      workflow.WorkflowRunRepository
	engine       *RunEngine
}

func NewWorkflowManager(
	workflowRepo workflow.WorkflowRepository,
	runRepo workflow.WorkflowRunRepository,
	engine *RunEngine,
) workflow.WorkflowManager {
	return &workflowManager{
		workflowRepo: workflowRepo,
		runRepo:      runRepo,
		engine:       engine,
	}
}

func (m *workflowManager) Tick() {
	now := time.Now().UTC()

	runs, err := m.runRepo.FindWakeableRuns(now.UnixMilli(), 50)
	if err != nil {
		log.Printf("[workflow] tick: failed to find wakeable runs: %v", err)
		return
	}

	for _, run := range runs {
		m.resumeRun(run)
	}

	cutoff := now.Add(-time.Duration(workflow.StuckRunTimeoutMinutes) * time.Minute).Unix()
	stuckRuns, err := m.runRepo.FindStuckRuns(cutoff)
	if err != nil {
		log.Printf("[workflow] tick: failed to find stuck runs: %v", err)
		return
	}

	for _, run := range stuckRuns {
		log.Printf("[workflow] tick: marking stuck run %s as error", run.ID)
		run.SetError("run stuck: not updated within timeout")
		if err := m.runRepo.Update(run); err != nil {
			log.Printf("[workflow] tick: failed to update stuck run %s: %v", run.ID, err)
		}
	}
}

func (m *workflowManager) resumeRun(run *workflow.WorkflowRun) {
	if !m.engine.TryLockRun(run.ID) {
		log.Printf("[workflow] tick: skipping run %s, already locked by another process", run.ID)
		return
	}
	defer m.engine.UnlockRun(run.ID)

	if err := resumeRunFromCurrent(m.workflowRepo, m.runRepo, m.engine, run); err != nil {
		log.Printf("[workflow] tick: failed to resume run %s: %v", run.ID, err)
	}
}
