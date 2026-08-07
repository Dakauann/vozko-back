package workflow_usecase

import (
	"testing"

	"vozko/domain/workflow"
)

// One inbound message must advance a conversation once.
//
// A menu flow parks at wait_for_reply. The tap resumes it, and the resume runs
// the engine synchronously, so a branch that reaches an end node leaves the run
// COMPLETED before the trigger loop looks for it. FindActiveByEntryAndTrigger
// then finds no active run, the "already resumed" guard never fires, and the
// SAME tap starts a fresh run from the workflow's entry node, which re-sends the
// menu. On Telegram that looked exactly like the bot re-showing the menu the
// instant an option was chosen, as if the contact had typed again after
// clicking.
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

	// The run the tap woke: parked at the menu and, by the time the loop below
	// runs, already finished. That is what the repository reports.
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
	te := &triggerEvaluator{
		workflowRepo: stubWorkflowRepo{byID: map[string]*workflow.Workflow{wf.ID: wf}, active: []*workflow.Workflow{wf}},
		runRepo:      runs,
		engine:       NewRunEngine(runs, stubRunLogRepo{}, NewNodeExecutorRegistry()),
	}

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

// The suppression is scoped to the one workflow that took the message. A run
// parked by a DIFFERENT workflow must not stop this one from starting, otherwise
// a stale parked run on the entry would silently mute the account's real
// workflow for as long as it sat there.
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

	// A leftover parked run belonging to a workflow this account is NOT linked to.
	waiting := &workflow.WorkflowRun{
		ID:            "run-stale",
		WorkflowID:    stale.ID,
		WorkspaceID:   workspaceID,
		EntryID:       entryID,
		Status:        workflow.RunStatusWaiting,
		WaitReason:    workflow.WaitReasonReply,
		CurrentNodeID: "menu",
		State:         workflow.NewRunState(),
	}

	runs := &recordingRunRepo{waitingReply: waiting}
	te := &triggerEvaluator{
		workflowRepo: stubWorkflowRepo{
			byID:   map[string]*workflow.Workflow{stale.ID: stale, linked.ID: linked},
			active: []*workflow.Workflow{stale, linked},
		},
		runRepo: runs,
		engine:  NewRunEngine(runs, stubRunLogRepo{}, NewNodeExecutorRegistry()),
	}

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

// --- stubs: only the methods Evaluate reaches need real behaviour ---

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
}

func (r *recordingRunRepo) FindWaitingReplyByEntry(string) (*workflow.WorkflowRun, error) {
	return r.waitingReply, nil
}

// nil: the woken run completed during the resume, so it is no longer active.
func (r *recordingRunRepo) FindActiveByEntryAndTrigger(string, string, string) (*workflow.WorkflowRun, error) {
	return nil, nil
}

func (r *recordingRunRepo) Create(run *workflow.WorkflowRun) error {
	r.created++
	r.createdFor = append(r.createdFor, run.WorkflowID)
	return nil
}
func (r *recordingRunRepo) Update(*workflow.WorkflowRun) error { return nil }

type stubRunLogRepo struct{ workflow.WorkflowRunLogRepository }

func (stubRunLogRepo) Create(*workflow.WorkflowRunLog) error { return nil }
