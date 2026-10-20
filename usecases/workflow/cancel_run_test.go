package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workflow"
)

type snapshotGuard struct {
	*MockWorkflowRunRepository
	updates int
}

func (s *snapshotGuard) Update(run *workflow.WorkflowRun) error {
	s.updates++
	return s.MockWorkflowRunRepository.Update(run)
}

func cancelFixture(t *testing.T, status workflow.RunStatus) (*snapshotGuard, *MockWorkflowRunLogRepository, *metricsSpy, workflow.CancelRunUseCase) {
	t.Helper()
	runs := &snapshotGuard{MockWorkflowRunRepository: NewMockWorkflowRunRepository()}
	state := workflow.NewRunState()
	state.Set("progress", "step-3")
	_ = runs.Create(&workflow.WorkflowRun{ID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp", Status: status, CurrentNodeID: "n3", State: state})
	logs := NewMockWorkflowRunLogRepository()
	recorder := &metricsSpy{}
	return runs, logs, recorder, NewCancelRunUseCase(runs, runs, logs, recorder, seesEverything{})
}

func TestCancellingARunWritesOnlyTheStatusAndTracesIt(t *testing.T) {
	runs, logs, recorder, uc := cancelFixture(t, workflow.RunStatusWaiting)

	if err := uc.Execute(viewer, "ws-1", "run-1"); err != nil {
		t.Fatal(err)
	}
	if runs.updates != 0 {
		t.Fatal("a cancel must never rewrite the whole row from the snapshot it read")
	}
	stored, _ := runs.FindByID("run-1")
	if stored.Status != workflow.RunStatusCancelled || stored.State.GetString("progress") != "step-3" {
		t.Fatalf("stored = %+v", stored)
	}
	steps, _ := logs.FindByRunID("run-1")
	if len(steps) != 1 || steps[0].Status != workflow.LogStatusCancelled || steps[0].NodeID != "n3" || steps[0].ErrorKind != workflow.ErrorKindCancelled {
		t.Fatalf("steps = %+v, want one cancelled step at the current node", steps)
	}
	if len(recorder.finished) != 1 || recorder.finished[0] != string(workflow.RunStatusCancelled) {
		t.Fatalf("finished metric = %v", recorder.finished)
	}
}

func TestCancellingARunThatFinishedMeanwhileChangesNothing(t *testing.T) {
	runs, logs, recorder, uc := cancelFixture(t, workflow.RunStatusWaiting)
	runs.runs["run-1"].Status = workflow.RunStatusWaiting
	runs.finishBeforeCancel = true

	err := uc.Execute(viewer, "ws-1", "run-1")
	if !errors.Is(err, workflow.ErrRunTerminal) {
		t.Fatalf("err = %v, want ErrRunTerminal", err)
	}
	stored, _ := runs.FindByID("run-1")
	if stored.Status != workflow.RunStatusCompleted {
		t.Fatalf("status = %s, the finished run must stay finished", stored.Status)
	}
	if steps, _ := logs.FindByRunID("run-1"); len(steps) != 0 || len(recorder.finished) != 0 {
		t.Fatalf("steps = %+v finished = %v", steps, recorder.finished)
	}
}
