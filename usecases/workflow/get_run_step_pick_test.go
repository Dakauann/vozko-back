package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workflow"
)

type pickingLogs struct {
	*MockWorkflowRunLogRepository
	asked []workflow.StepPick
	steps []*workflow.WorkflowRunLog
}

func (p *pickingLogs) PickSteps(runID string, pick workflow.StepPick) ([]*workflow.WorkflowRunLog, error) {
	p.asked = append(p.asked, pick)
	return p.steps, nil
}

func TestAPickedReadReturnsOnlyTheStepsTheRepositoryPicked(t *testing.T) {
	runs, _ := runOwnedBy("ws-owner")
	logs := &pickingLogs{MockWorkflowRunLogRepository: NewMockWorkflowRunLogRepository(), steps: []*workflow.WorkflowRunLog{
		{ID: "s-1", RunID: "run-1", NodeID: "login", Status: workflow.LogStatusExecuted, Output: map[string]interface{}{"api_token": "abc"}},
	}}
	detail, err := NewGetRunUseCase(runs, logs, seesEverything{}).PickSteps(viewer, "ws-owner", "run-1", workflow.PinPick("login", ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Logs) != 1 || detail.Logs[0].ID != "s-1" || detail.Run == nil {
		t.Fatalf("detail = %+v", detail)
	}
	if detail.Logs[0].Output["api_token"] == "abc" {
		t.Fatal("picked steps are redacted like the full read")
	}
	if len(logs.asked) != 1 || logs.asked[0].LatestOfNodes[0] != "login" {
		t.Fatalf("asked = %+v", logs.asked)
	}
}

func TestATriggerStepIsNotReadWhenTheRunKeptItsTriggerData(t *testing.T) {
	_, run := runOwnedBy("ws-owner")
	run.TriggerData = map[string]interface{}{"cpf_final": "25"}
	runs := NewMockWorkflowRunRepository()
	_ = runs.Create(run)
	logs := &pickingLogs{MockWorkflowRunLogRepository: NewMockWorkflowRunLogRepository()}

	if _, err := NewGetRunUseCase(runs, logs, seesEverything{}).PickSteps(viewer, "ws-owner", "run-1", workflow.StepPick{LatestOfNodes: []string{"login"}, Trigger: true}); err != nil {
		t.Fatal(err)
	}
	if len(logs.asked) != 1 || logs.asked[0].Trigger {
		t.Fatalf("asked = %+v, the stored trigger data already seeds the reproduction", logs.asked)
	}
}

func TestAPickedReadIsRefusedForAHiddenRunOrWithoutAPicker(t *testing.T) {
	runs, _ := runOwnedBy("ws-owner")
	logs := &pickingLogs{MockWorkflowRunLogRepository: NewMockWorkflowRunLogRepository()}
	if _, err := NewGetRunUseCase(runs, logs, inboxRule{err: errors.New("db down")}).PickSteps(viewer, "ws-other", "run-1", workflow.PinPick("login", "")); !errors.Is(err, workflow.ErrRunNotFound) {
		t.Fatalf("err = %v", err)
	}
	if len(logs.asked) != 0 {
		t.Fatal("a hidden run must not reach the step store")
	}
	if _, err := NewGetRunUseCase(runs, nonPickingLogs{NewMockWorkflowRunLogRepository()}, seesEverything{}).PickSteps(viewer, "ws-owner", "run-1", workflow.PinPick("login", "")); !errors.Is(err, ErrRunStepsUnpickable) {
		t.Fatalf("err = %v, a store that cannot pick steps must refuse instead of reading the whole run", err)
	}
}

type nonPickingLogs struct {
	workflow.WorkflowRunLogRepository
}
