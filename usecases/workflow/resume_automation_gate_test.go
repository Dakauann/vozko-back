package workflow_usecase

import (
	"testing"

	"vozko/domain/workflow"
)

type gateStub struct {
	enabled  bool
	asked    int
	lastID   string
	lastType string
}

func (g *gateStub) AutomationEnabled(entryID, entryType string) bool {
	g.asked++
	g.lastID, g.lastType = entryID, entryType
	return g.enabled
}

type runRepoStub struct {
	workflow.WorkflowRunRepository
	updated *workflow.WorkflowRun
	updates int
}

func (r *runRepoStub) Update(run *workflow.WorkflowRun) error {
	r.updated = run
	r.updates++
	return nil
}

type workflowRepoStub struct {
	workflow.WorkflowRepository
	t       *testing.T
	fetched bool
}

func (w *workflowRepoStub) FindByID(string) (*workflow.Workflow, error) {
	w.fetched = true
	w.t.Error("the workflow graph was loaded for a run that should have been cancelled")
	return nil, nil
}

func parkedRun() *workflow.WorkflowRun {
	return &workflow.WorkflowRun{
		ID:            "run-1",
		WorkflowID:    "wf-1",
		EntryID:       "entry-1",
		EntryType:     "whatsapp",
		Status:        workflow.RunStatusWaiting,
		CurrentNodeID: "node-7",
	}
}

func TestResumeCancelsWhenAutomationWasTurnedOff(t *testing.T) {
	gate := &gateStub{enabled: false}
	engine := NewRunEngine(nil, nil, nil)
	engine.SetAutomationGate(gate)

	runs := &runRepoStub{}
	wfs := &workflowRepoStub{t: t}
	run := parkedRun()

	if err := resumeRunFromCurrent(wfs, runs, engine, run); err != nil {
		t.Fatalf("cancelling is a normal outcome, not an error: %v", err)
	}

	if run.Status != workflow.RunStatusCancelled {
		t.Errorf("status = %q, want cancelled — an error would retry and eventually deliver the very message the operator silenced", run.Status)
	}
	if runs.updates != 1 || runs.updated != run {
		t.Errorf("the cancellation must be persisted once, got %d update(s)", runs.updates)
	}
	if wfs.fetched {
		t.Error("no work may happen after the gate says no")
	}
	if gate.asked != 1 {
		t.Errorf("gate consulted %d times, want exactly 1", gate.asked)
	}
	if gate.lastID != "entry-1" || gate.lastType != "whatsapp" {
		t.Errorf("gate asked about %q/%q, want the run's own entry", gate.lastID, gate.lastType)
	}
}

func TestResumeProceedsWhenAutomationIsOn(t *testing.T) {
	engine := NewRunEngine(nil, nil, nil)
	engine.SetAutomationGate(&gateStub{enabled: true})

	run := parkedRun()
	runs := &runRepoStub{}
	wfs := &passthroughWorkflowRepo{}

	_ = resumeRunFromCurrent(wfs, runs, engine, run)

	if !wfs.fetched {
		t.Error("the resume stopped at the gate even though automation was on")
	}
	if run.Status == workflow.RunStatusCancelled {
		t.Error("a run was cancelled while automation was on")
	}
}

type passthroughWorkflowRepo struct {
	workflow.WorkflowRepository
	fetched bool
}

func (w *passthroughWorkflowRepo) FindByID(string) (*workflow.Workflow, error) {
	w.fetched = true
	return nil, nil
}

func TestResumeIsUnchangedWithoutAGate(t *testing.T) {
	engine := NewRunEngine(nil, nil, nil)

	run := parkedRun()
	wfs := &passthroughWorkflowRepo{}
	_ = resumeRunFromCurrent(wfs, &runRepoStub{}, engine, run)

	if !wfs.fetched {
		t.Error("a nil gate blocked a resume; it must allow everything")
	}
}

func TestAutomationOffDefaultsToAllowing(t *testing.T) {
	var nilEngine *RunEngine
	if nilEngine.automationOff("entry-1", "whatsapp") {
		t.Error("a nil engine must not report automation as off")
	}

	engine := NewRunEngine(nil, nil, nil)
	if engine.automationOff("entry-1", "whatsapp") {
		t.Error("no gate wired must not report automation as off")
	}

	engine.SetAutomationGate(&gateStub{enabled: false})
	if engine.automationOff("", "whatsapp") {
		t.Error("an empty entry id has nothing to check and must be allowed")
	}
	if !engine.automationOff("entry-1", "whatsapp") {
		t.Error("an explicit disabled answer must stop the run")
	}
}
