package workflow_usecase

import (
	"testing"

	"vozko/domain/workflow"
)

func TestEvaluateDoesNotStartASecondRunForAMessageThatResumedAWaitingRun(t *testing.T) {
	const workflowID, entryID, workspaceID = "wf-1", "entry-1", "ws-1"

	wf := &workflow.Workflow{
		ID:          workflowID,
		WorkspaceID: workspaceID,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "trigger-1", Type: workflow.NodeTypeTriggerMessageReceived},
				{ID: "menu", Type: workflow.NodeTypeActionSendInteractive},
			},
			Edges: []workflow.Edge{{Source: "trigger-1", Target: "menu"}},
		},
	}

	waiting := &workflow.WorkflowRun{
		ID:            "run-woken",
		WorkflowID:    workflowID,
		WorkspaceID:   workspaceID,
		EntryID:       entryID,
		Status:        workflow.RunStatusWaiting,
		WaitReason:    workflow.WaitReasonReply,
		CurrentNodeID: "menu",
		State:         workflow.NewRunState(),
	}

	runs := &recordingRunRepo{waitingReply: waiting}
	te := NewTriggerEvaluator(
		stubWorkflowRepo{byID: map[string]*workflow.Workflow{wf.ID: wf}, active: []*workflow.Workflow{wf}},
		runs, NewRunEngine(runs, stubRunLogRepo{}, NewNodeExecutorRegistry()), nil,
	)

	te.Evaluate(workflow.TriggerEvent{
		WorkspaceID: workspaceID,
		EntryID:     entryID,
		EntryType:   "telegram",
		TriggerType: workflow.TriggerMessageReceived,
		Data: map[string]interface{}{
			"message":             "Falar com Atendente",
			"account_workflow_id": workflowID,
		},
	})

	if runs.created > 0 {
		t.Errorf("created %d new run(s) for a message that had already resumed run %s; the menu is re-sent to the contact",
			runs.created, waiting.ID)
	}
}

func TestEvaluateStillStartsOtherWorkflowsWhenAnotherOnesRunWasResumed(t *testing.T) {
	const entryID, workspaceID = "entry-1", "ws-1"

	menu := workflow.Graph{
		Nodes: []workflow.Node{
			{ID: "trigger-1", Type: workflow.NodeTypeTriggerMessageReceived},
			{ID: "menu", Type: workflow.NodeTypeActionSendInteractive},
		},
		Edges: []workflow.Edge{{Source: "trigger-1", Target: "menu"}},
	}
	stale := &workflow.Workflow{ID: "wf-stale", WorkspaceID: workspaceID, Graph: menu}
	linked := &workflow.Workflow{ID: "wf-linked", WorkspaceID: workspaceID, Graph: menu}

	waiting := &workflow.WorkflowRun{
		ID:            "run-stale",
		EntryType:     "telegram",
		WorkflowID:    stale.ID,
		WorkspaceID:   workspaceID,
		EntryID:       entryID,
		Status:        workflow.RunStatusWaiting,
		WaitReason:    workflow.WaitReasonReply,
		CurrentNodeID: "menu",
		State:         workflow.NewRunState(),
	}

	runs := &recordingRunRepo{waitingReply: waiting}
	te := NewTriggerEvaluator(
		stubWorkflowRepo{
			byID:   map[string]*workflow.Workflow{stale.ID: stale, linked.ID: linked},
			active: []*workflow.Workflow{stale, linked},
		},
		runs, NewRunEngine(runs, stubRunLogRepo{}, NewNodeExecutorRegistry()), nil,
	)

	te.Evaluate(workflow.TriggerEvent{
		WorkspaceID: workspaceID,
		EntryID:     entryID,
		EntryType:   "telegram",
		TriggerType: workflow.TriggerMessageReceived,
		Data: map[string]interface{}{
			"message":             "oi",
			"account_workflow_id": linked.ID,
		},
	})

	if runs.created != 1 {
		t.Fatalf("created %d runs, want exactly 1 (the account's linked workflow)", runs.created)
	}
	if runs.createdFor[0] != linked.ID {
		t.Errorf("started a run for workflow %q, want %q", runs.createdFor[0], linked.ID)
	}
}

type stubWorkflowRepo struct {
	workflow.WorkflowRepository
	byID   map[string]*workflow.Workflow
	active []*workflow.Workflow
}

func (r stubWorkflowRepo) FindByID(id string) (*workflow.Workflow, error) { return r.byID[id], nil }

func (r stubWorkflowRepo) FindActiveByTrigger(string, workflow.TriggerType) ([]*workflow.Workflow, error) {
	return r.active, nil
}

type recordingRunRepo struct {
	workflow.WorkflowRunRepository
	waitingReply *workflow.WorkflowRun
	created      int
	createdFor   []string
	lookups      int
	updates      int
}

func (r *recordingRunRepo) FindWaitingReplyByEntry(string) (*workflow.WorkflowRun, error) {
	r.lookups++
	return r.waitingReply, nil
}

func (r *recordingRunRepo) FindActiveByEntryAndTrigger(string, string, string) (*workflow.WorkflowRun, error) {
	return nil, nil
}

func (r *recordingRunRepo) Create(run *workflow.WorkflowRun) error {
	r.created++
	r.createdFor = append(r.createdFor, run.WorkflowID)
	return nil
}
func (r *recordingRunRepo) Update(*workflow.WorkflowRun) error {
	r.updates++
	return nil
}

type stubRunLogRepo struct {
	workflow.WorkflowRunLogRepository
}

func (stubRunLogRepo) Create(*workflow.WorkflowRunLog) error { return nil }
