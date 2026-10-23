package workflow_repository

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/workflow"
	"vozko/infra/database/schema"
)

func TestDraftSavesAgainstPostgres(t *testing.T) {
	db := postgresSchemaTx(t, &schema.WorkflowSchema{}, &schema.WorkflowVersionSchema{}, &schema.WorkflowRunSchema{})
	repo := NewWorkflowRepository(db)
	stored := &workflow.Workflow{ID: uuid.NewString(), WorkspaceID: uuid.NewString(), Name: "Fluxo", Status: workflow.WorkflowStatusActive, Type: workflow.WorkflowTypeMessages, Version: 1, Graph: manualTriggerGraph("end")}
	if err := repo.Create(stored); err != nil {
		t.Fatal(err)
	}

	person := *stored
	person.Graph = manualTriggerGraph("pessoa")
	revision, err := repo.SaveDraft(&person, 1)
	if err != nil || revision != 2 {
		t.Fatalf("first save = %d, %v, want revision 2", revision, err)
	}

	elo := *stored
	elo.Graph = manualTriggerGraph("elo")
	if _, err := repo.SaveDraft(&elo, 1); err == nil {
		t.Fatal("a second save from revision 1 must be refused")
	} else if current, ok := workflow.CurrentDraftRevisionOf(err); !ok || current != 2 {
		t.Fatalf("err = %v, want a conflict naming revision 2", err)
	}

	got, err := repo.FindByID(stored.ID)
	if err != nil || got.Version != 2 || got.Graph.FindNode("pessoa") == nil || got.Graph.FindNode("elo") != nil {
		t.Fatalf("stored = %+v, %v, want the first save kept", got, err)
	}
	if got.Status != workflow.WorkflowStatusActive {
		t.Fatalf("status = %s, saving the draft must not touch the status", got.Status)
	}

	if err := repo.UpdateDepartment(stored.ID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if again, _ := repo.FindByID(stored.ID); again.Version != 2 || again.Graph.FindNode("pessoa") == nil {
		t.Fatalf("a department change must keep the draft and its revision: %+v", again)
	}

	gone := *stored
	gone.ID = uuid.NewString()
	if _, err := repo.SaveDraft(&gone, 1); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
	if err := repo.Delete(stored.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveDraft(&person, 2); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("a deleted workflow must read as not found: %v", err)
	}
}

func manualTriggerGraph(endID string) workflow.Graph {
	return workflow.Graph{
		Nodes: []workflow.Node{{ID: "t", Type: workflow.NodeTypeTriggerMessageReceived}, {ID: endID, Type: workflow.NodeTypeEnd}},
		Edges: []workflow.Edge{{Source: "t", Target: endID}},
	}
}
