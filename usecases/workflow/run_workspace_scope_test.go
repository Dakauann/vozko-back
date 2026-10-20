package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workflow"
)

func runOwnedBy(workspaceID string) (*MockWorkflowRunRepository, *workflow.WorkflowRun) {
	runs := NewMockWorkflowRunRepository()
	run := &workflow.WorkflowRun{
		ID:          "run-1",
		WorkflowID:  "wf-1",
		WorkspaceID: workspaceID,
		Status:      workflow.RunStatusWaiting,
		State:       workflow.NewRunState(),
	}
	_ = runs.Create(run)
	return runs, run
}

func TestGetRunHidesRunsOfOtherWorkspacesSoStateAndTokensDoNotLeak(t *testing.T) {
	cases := []struct {
		name      string
		workspace string
		wantErr   error
	}{
		{"owner reads its run", "ws-owner", nil},
		{"another workspace gets not found", "ws-other", workflow.ErrRunNotFound},
		{"no workspace is refused", "", workflow.ErrRunNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runs, _ := runOwnedBy("ws-owner")
			detail, err := NewGetRunUseCase(runs, NewMockWorkflowRunLogRepository(), seesEverything{}).Execute(viewer, c.workspace, "run-1")
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if c.wantErr != nil && detail != nil {
				t.Fatalf("a refused read must not return the run: %+v", detail)
			}
			if c.wantErr == nil && (detail == nil || detail.Run.ID != "run-1") {
				t.Fatalf("owner did not get its run: %+v", detail)
			}
		})
	}
}

func TestCancelRunOnlyCancelsRunsOfTheCallersWorkspace(t *testing.T) {
	cases := []struct {
		name       string
		workspace  string
		wantErr    error
		wantStatus workflow.RunStatus
	}{
		{"owner cancels its run", "ws-owner", nil, workflow.RunStatusCancelled},
		{"another workspace gets not found", "ws-other", workflow.ErrRunNotFound, workflow.RunStatusWaiting},
		{"no workspace is refused", "", workflow.ErrRunNotFound, workflow.RunStatusWaiting},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runs, _ := runOwnedBy("ws-owner")
			err := NewCancelRunUseCase(runs, runs, NewMockWorkflowRunLogRepository(), nil, seesEverything{}).Execute(viewer, c.workspace, "run-1")
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			stored, _ := runs.FindByID("run-1")
			if stored.Status != c.wantStatus {
				t.Fatalf("status = %q, want %q", stored.Status, c.wantStatus)
			}
		})
	}
}
