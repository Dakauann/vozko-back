package workflow_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/workflow"
)

func runWithSteps(t *testing.T, steps int) (*MockWorkflowRunRepository, *MockWorkflowRunLogRepository) {
	t.Helper()
	runs, _ := runOwnedBy("ws-owner")
	logs := NewMockWorkflowRunLogRepository()
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for i := 0; i < steps; i++ {
		_ = logs.Create(&workflow.WorkflowRunLog{ID: string(rune('a' + i)), RunID: "run-1", NodeID: string(rune('a' + i)), Status: workflow.LogStatusExecuted, ExecutedAt: at.Add(time.Duration(i) * time.Second)})
	}
	return runs, logs
}

func TestTheRunDetailPagesItsSteps(t *testing.T) {
	runs, logs := runWithSteps(t, 3)
	uc := NewGetRunUseCase(runs, logs, seesEverything{}).(workflow.RunDetailPages)

	detail, err := uc.ExecutePage(viewer, "ws-owner", "run-1", workflow.StepWindow{Offset: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Logs) != 1 || detail.Logs[0].NodeID != "b" {
		t.Fatalf("logs = %+v, want only the second step", detail.Logs)
	}
	if detail.Steps == nil || *detail.Steps != (workflow.RunStepsPage{Offset: 1, Limit: 1, Total: 3, HasMore: true}) {
		t.Fatalf("steps = %+v", detail.Steps)
	}
}

func TestASmallRunStillComesWithEveryStepByDefault(t *testing.T) {
	runs, logs := runWithSteps(t, 3)
	detail, err := NewGetRunUseCase(runs, logs, seesEverything{}).(workflow.RunDetailPages).ExecutePage(viewer, "ws-owner", "run-1", workflow.StepWindow{})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Logs) != 3 || detail.Steps == nil || detail.Steps.HasMore || detail.Steps.Total != 3 || detail.Steps.Limit != workflow.DefaultRunStepsPage {
		t.Fatalf("detail = %+v steps = %+v", detail.Logs, detail.Steps)
	}
}

func TestAStepPageNeverExceedsTheBoundAndAHiddenRunStaysHidden(t *testing.T) {
	if got := (workflow.StepWindow{Offset: -4, Limit: 1_000_000}).Normalized(); got != (workflow.StepWindow{Offset: 0, Limit: workflow.MaxRunStepsPage}) {
		t.Fatalf("window = %+v", got)
	}
	runs, logs := runWithSteps(t, 1)
	_, err := NewGetRunUseCase(runs, logs, inboxRule{err: errors.New("db down")}).(workflow.RunDetailPages).ExecutePage(viewer, "ws-other", "run-1", workflow.StepWindow{})
	if !errors.Is(err, workflow.ErrRunNotFound) {
		t.Fatalf("err = %v", err)
	}
}
