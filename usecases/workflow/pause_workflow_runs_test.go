package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workflow"
)

type failingWorkflowCancellation struct {
	workflow.WorkflowRunRepository
	err error
}

func (r *failingWorkflowCancellation) CancelByWorkflow(id string) (int64, error) {
	if r.err != nil {
		return 0, r.err
	}
	return r.WorkflowRunRepository.CancelByWorkflow(id)
}

func TestPausingCancelsRunningAndWaitingRunsOnlyOfTheSelectedWorkflow(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusActive, manualGraph("end"))
	runs := NewMockWorkflowRunRepository()
	for _, run := range []*workflow.WorkflowRun{
		{ID: "running", WorkflowID: "wf-1", Status: workflow.RunStatusRunning},
		{ID: "waiting", WorkflowID: "wf-1", Status: workflow.RunStatusWaiting},
		{ID: "webhook-other-workflow", WorkflowID: "wf-2", Status: workflow.RunStatusRunning},
	} {
		if err := runs.Create(run); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewPauseWorkflowUseCase(repo, runs).Execute("wf-1"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"running", "waiting"} {
		run, _ := runs.FindByID(id)
		if run.Status != workflow.RunStatusCancelled {
			t.Fatalf("%s remained active after pause: %+v", id, run)
		}
	}
	other, _ := runs.FindByID("webhook-other-workflow")
	if other.Status != workflow.RunStatusRunning {
		t.Fatal("pausing this workflow stopped an independent workflow")
	}
}

func TestFailedPauseCancellationReturnsAnErrorAndCanBeRetried(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusActive, manualGraph("end"))
	runs := &failingWorkflowCancellation{WorkflowRunRepository: NewMockWorkflowRunRepository(), err: errors.New("database unavailable")}
	uc := NewPauseWorkflowUseCase(repo, runs)
	if _, err := uc.Execute("wf-1"); !errors.Is(err, runs.err) {
		t.Fatalf("cancellation failure was hidden: %v", err)
	}
	w, _ := repo.FindByID("wf-1")
	if w.Status != workflow.WorkflowStatusPaused {
		t.Fatal("new workflow triggers were left enabled after cancellation failed")
	}
	runs.err = nil
	if _, err := uc.Execute("wf-1"); err != nil {
		t.Fatalf("retrying the pause must finish cancellation: %v", err)
	}
}
