package workflow_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/workflow"
)

type alertRecorderSpy struct {
	signals []workflow.RuntimeAlertSignal
	err     error
}

func (s *alertRecorderSpy) RecordRuntimeAlert(signal workflow.RuntimeAlertSignal) error {
	s.signals = append(s.signals, signal)
	return s.err
}

func TestTheStuckRunSweepRaisesARunStuckAlertForEachRunItErrors(t *testing.T) {
	runs := NewMockWorkflowRunRepository()
	stale := time.Now().UTC().Add(-time.Hour)
	_ = runs.Create(&workflow.WorkflowRun{ID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "e-1", Status: workflow.RunStatusRunning, UpdatedAt: stale, State: workflow.NewRunState()})
	engine := NewRunEngine(runs, NewMockWorkflowRunLogRepository(), NewNodeExecutorRegistry())
	spy := &alertRecorderSpy{err: errors.New("db down")}
	engine.SetRuntimeAlerts(spy)

	NewWorkflowManager(NewMockWorkflowRepository(), runs, engine).Tick()

	if len(spy.signals) != 1 {
		t.Fatalf("signals = %+v, want one stuck run alert", spy.signals)
	}
	got := spy.signals[0]
	if got.Key.Kind != workflow.AlertRunStuck || got.Key.WorkflowID != "wf-1" || got.Key.WorkspaceID != "ws-1" || got.Sample != "run-1" {
		t.Fatalf("signal = %+v", got)
	}
	if stored, _ := runs.FindByID("run-1"); stored.Status != workflow.RunStatusError {
		t.Fatalf("a failing alert write must not undo the sweep: %+v", stored)
	}
}

func TestAFailedWaitingRunLookupRaisesTheInboundAlertForTheWorkspace(t *testing.T) {
	engine := NewRunEngine(&recordingRunRepo{}, stubRunLogRepo{}, NewNodeExecutorRegistry())
	spy := &alertRecorderSpy{}
	engine.SetRuntimeAlerts(spy)
	waiters := NewReplyWaiters(stubWorkflowRepo{}, lookupFailingRuns{&recordingRunRepo{}}, engine)

	if _, err := waiters.WaitingForReply(messageEvent()); err == nil {
		t.Fatal("the lookup error must still reach the caller, which then replies to nobody")
	}
	if len(spy.signals) != 1 {
		t.Fatalf("signals = %+v", spy.signals)
	}
	got := spy.signals[0]
	if got.Key.Kind != workflow.AlertInboundLookupFailed || got.Key.WorkspaceID != "ws-1" || got.Key.Subject != "telegram" || got.Sample != "entry-1" {
		t.Fatalf("signal = %+v", got)
	}
}
