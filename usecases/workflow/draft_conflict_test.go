package workflow_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/shared"
	"vozko/domain/workflow"
	workspace_department "vozko/domain/workspace/workspace_department"
)

func revisionOf(repo *MockWorkflowRepository, workflowID string) int {
	stored, _ := repo.FindByID(workflowID)
	if stored == nil {
		return 0
	}
	return stored.Version
}

func draftEditFixture(t *testing.T) (*MockWorkflowRepository, workflow.ScopedWorkflowsUseCase) {
	t.Helper()
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusDraft, manualGraph("end"))
	return repo, NewScopedWorkflowsUseCase(ScopedWorkflowsDeps{
		Get: NewGetWorkflowUseCase(repo), Update: NewUpdateWorkflowUseCase(repo), Versions: NewWorkflowVersionsUseCase(repo),
	})
}

func TestSavingTheDraftAnswersTheNextRevision(t *testing.T) {
	_, uc := draftEditFixture(t)
	saved, err := uc.Update(context.Background(), ownerScope("u-1"), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: manualGraph("fim")}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version != 2 {
		t.Fatalf("revision = %d, want 2 so the editor saves from it next", saved.Version)
	}
}

func TestTheLaterOfTwoSavesFromTheSameRevisionGetsTheConflictInsteadOfOverwriting(t *testing.T) {
	repo, uc := draftEditFixture(t)
	if _, err := uc.Update(context.Background(), ownerScope("u-person"), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: manualGraph("pessoa")}, 1); err != nil {
		t.Fatal(err)
	}
	_, err := uc.Update(context.Background(), ownerScope("u-elo"), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: manualGraph("elo")}, 1)
	if current, ok := workflow.CurrentDraftRevisionOf(err); !ok || current != 2 {
		t.Fatalf("err = %v, want a draft conflict at revision 2", err)
	}
	stored, _ := repo.FindByID("wf-1")
	if stored.Graph.FindNode("pessoa") == nil || stored.Graph.FindNode("elo") != nil {
		t.Fatal("the first save must stay; the later writer must not overwrite it")
	}
}

func TestASaveThatDoesNotSayWhichRevisionItEditedIsRefused(t *testing.T) {
	repo, uc := draftEditFixture(t)
	_, err := uc.Update(context.Background(), ownerScope("u-1"), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: manualGraph("fim")}, 0)
	if !errors.Is(err, workflow.ErrDraftRevisionRequired) {
		t.Fatalf("err = %v, want the revision required (fail closed)", err)
	}
	stored, _ := repo.FindByID("wf-1")
	if stored.Version != 1 || stored.Graph.FindNode("fim") != nil {
		t.Fatal("nothing may be written without a revision")
	}
}

func TestRestoringAVersionOverAnEditSavedMeanwhileIsAConflict(t *testing.T) {
	repo, uc := versionsFixture(t)
	stored, _ := repo.FindByID("wf-1")
	if _, err := uc.RestoreVersion(context.Background(), ownerScope("u-1"), "wf-1", 1, stored.Version-1); !errors.Is(err, workflow.ErrDraftConflict) {
		t.Fatalf("err = %v, want a draft conflict", err)
	}
	restored, err := uc.RestoreVersion(context.Background(), ownerScope("u-1"), "wf-1", 1, stored.Version)
	if err != nil || restored.Version != stored.Version+1 {
		t.Fatalf("restored = %+v, err = %v, want the next revision", restored, err)
	}
}

func TestPinningFromARunOverAnEditSavedMeanwhileIsAConflict(t *testing.T) {
	runs := &runDetailStub{detail: recordedRun(map[string]interface{}{"a": "b"})}
	uc, repo := pinningFixture(t, runs)
	_, err := uc.PinFromRun(context.Background(), ownerScope("u-1"), shared.Person{UserID: "u-1"}, PinFromRunInput{WorkflowID: "wf-1", NodeID: "login", RunID: "run-9", Revision: 7})
	if !errors.Is(err, workflow.ErrDraftConflict) {
		t.Fatalf("err = %v, want a draft conflict", err)
	}
	stored, _ := repo.FindByID("wf-1")
	if stored.Graph.FindNode("login").Pinned != nil {
		t.Fatal("the pin must not be written over a newer draft")
	}
}

type fixedDepartment string

func (d fixedDepartment) Resolve(context.Context, string) (string, error) { return string(d), nil }

func TestAssigningADepartmentKeepsTheDraftAndItsRevision(t *testing.T) {
	repo, uc := draftEditFixture(t)
	if _, err := NewAssignDepartmentUseCase(repo, fixedDepartment("dep-1")).Execute(context.Background(), "wf-1"); err != nil {
		t.Fatal(err)
	}
	stored, _ := repo.FindByID("wf-1")
	if stored.DepartmentID != "dep-1" || stored.Version != 1 {
		t.Fatalf("stored = %+v, want the department set and the revision untouched", stored)
	}
	if _, err := uc.Update(context.Background(), ownerScope("u-1"), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: manualGraph("fim")}, 1); err != nil {
		t.Fatalf("an editor at revision 1 must still save after a department change: %v", err)
	}
}

var _ workspace_department.CreationDepartmentResolver = fixedDepartment("")
