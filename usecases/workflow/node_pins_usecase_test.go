package workflow_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/shared"
	"vozko/domain/workflow"
	wd "vozko/domain/workspace/workspace_department"
)

func pinningFixture(t *testing.T, runs *runDetailStub) (*NodePinsUseCase, *MockWorkflowRepository) {
	t.Helper()
	repo := NewMockWorkflowRepository()
	wf := debugWorkflow("https://api.example.com", nil)
	wf.Version = 1
	if err := repo.Create(wf); err != nil {
		t.Fatal(err)
	}
	scoped := NewScopedWorkflowsUseCase(ScopedWorkflowsDeps{
		Get:        NewGetWorkflowUseCase(repo),
		Update:     NewUpdateWorkflowUseCase(repo),
		Versions:   NewWorkflowVersionsUseCase(repo),
		PinSources: everyRunVisible{},
	})
	uc := NewNodePinsUseCase(scoped, runs)
	uc.now = pinTimeForTests
	return uc, repo
}

func TestPinningFromAProductionStepCopiesItsRecordedOutputAndBranchIntoTheDraft(t *testing.T) {
	runs := &runDetailStub{detail: recordedRun(map[string]interface{}{"a": "b"})}
	uc, repo := pinningFixture(t, runs)

	saved, err := uc.PinFromRun(context.Background(), ownerScope("u-1"), shared.Person{UserID: "u-1"}, PinFromRunInput{WorkflowID: "wf-1", NodeID: "login", RunID: "run-9", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	pin := saved.Graph.FindNode("login").Pinned
	if pin == nil || pin.Source != workflow.PinSourceOfRun("run-9") || pin.Edge != "falha" || pin.Output["status_code"] != 401.0 || !pin.PinnedAt.Equal(pinTimeForTests()) {
		t.Fatalf("pin = %+v", pin)
	}
	stored, _ := repo.FindByID("wf-1")
	if stored.Graph.FindNode("login").Pinned == nil {
		t.Fatal("the pin must be saved on the draft")
	}
	if runs.viewer.UserID != "u-1" {
		t.Fatal("the run must be read as the person, so the visibility rule applies")
	}
	if runs.fullReads != 0 || runs.pick == nil || !reflect.DeepEqual(*runs.pick, workflow.PinPick("login", "")) {
		t.Fatalf("pick = %+v, full reads = %d; a pin reads only the latest usable step of its node", runs.pick, runs.fullReads)
	}
}

func TestPinningAChosenStepReadsOnlyThatStep(t *testing.T) {
	detail := recordedRun(nil)
	detail.Logs[1].ID = "step-login"
	runs := &runDetailStub{detail: detail}
	uc, _ := pinningFixture(t, runs)

	if _, err := uc.PinFromRun(context.Background(), ownerScope("u-1"), shared.Person{UserID: "u-1"}, PinFromRunInput{WorkflowID: "wf-1", NodeID: "login", RunID: "run-9", StepID: "step-login", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if runs.fullReads != 0 || runs.pick == nil || runs.pick.StepID != "step-login" || len(runs.pick.LatestOfNodes) != 0 {
		t.Fatalf("pick = %+v, full reads = %d", runs.pick, runs.fullReads)
	}
}

func TestPinningFromAProductionStepIsRefusedWhenTheStepCannotBeUsed(t *testing.T) {
	otherFlow := recordedRun(nil)
	otherFlow.Run.WorkflowID = "wf-2"
	cases := []struct {
		name string
		runs *runDetailStub
		in   PinFromRunInput
		want error
	}{
		{"hidden or missing run", &runDetailStub{err: workflow.ErrRunNotFound}, PinFromRunInput{WorkflowID: "wf-1", NodeID: "login", RunID: "run-9"}, workflow.ErrRunNotFound},
		{"run of another workflow", &runDetailStub{detail: otherFlow}, PinFromRunInput{WorkflowID: "wf-1", NodeID: "login", RunID: "run-9"}, workflow.ErrRunNotFound},
		{"node never ran in that run", &runDetailStub{detail: recordedRun(nil)}, PinFromRunInput{WorkflowID: "wf-1", NodeID: "ok", RunID: "run-9"}, workflow.ErrPinStepUnusable},
		{"node gone from the draft", &runDetailStub{detail: func() *workflow.RunDetail {
			d := recordedRun(nil)
			d.Logs = append(d.Logs, &workflow.WorkflowRunLog{ID: "s-x", NodeID: "removed", Status: workflow.LogStatusExecuted})
			return d
		}()}, PinFromRunInput{WorkflowID: "wf-1", NodeID: "removed", RunID: "run-9"}, workflow.ErrPinNodeNotFound},
		{"chosen step of another node", &runDetailStub{detail: recordedRun(nil)}, PinFromRunInput{WorkflowID: "wf-1", NodeID: "login", RunID: "run-9", StepID: "nope"}, workflow.ErrPinStepUnusable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc, repo := pinningFixture(t, tc.runs)
			_, err := uc.PinFromRun(context.Background(), ownerScope("u-1"), shared.Person{UserID: "u-1"}, tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			stored, _ := repo.FindByID("wf-1")
			if stored.Graph.HasPins() {
				t.Fatal("a refused pin must leave the draft untouched")
			}
		})
	}
}

func TestReadingOneVersionReturnsItsGraphInsideTheScope(t *testing.T) {
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusDraft, manualGraph("end"))
	if _, err := activation(repo).Publish(publisherCtx(), "wf-1"); err != nil {
		t.Fatal(err)
	}
	scoped := NewScopedWorkflowsUseCase(ScopedWorkflowsDeps{Get: NewGetWorkflowUseCase(repo), Versions: NewWorkflowVersionsUseCase(repo)})

	v, err := scoped.Version(ownerScope("u-1"), "wf-1", 1)
	if err != nil || v.Graph == nil || v.Graph.FindNode("end") == nil {
		t.Fatalf("version = %+v err = %v", v, err)
	}
	if _, err := scoped.Version(ownerScope("u-1"), "wf-1", 9); !errors.Is(err, workflow.ErrVersionNotFound) {
		t.Fatalf("missing version err = %v", err)
	}
	if _, err := scoped.Version(workflow.Scope{WorkspaceID: "ws-other", Departments: &wd.DepartmentFilter{IsOwnerOrAdmin: true}}, "wf-1", 1); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("another workspace err = %v", err)
	}
}
