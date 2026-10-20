package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workflow"
)

type lookupFailingRuns struct {
	*recordingRunRepo
}

func (lookupFailingRuns) FindWaitingReplyByEntry(string) (*workflow.WorkflowRun, error) {
	return nil, errors.New("db down")
}

func replyMenuWorkflow() *workflow.Workflow {
	return &workflow.Workflow{
		ID:          "wf-1",
		WorkspaceID: "ws-1",
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "trigger-1", Type: workflow.NodeTypeTriggerMessageReceived},
				{ID: "menu", Type: workflow.NodeTypeActionSendInteractive},
			},
			Edges: []workflow.Edge{{Source: "trigger-1", Target: "menu"}},
		},
	}
}

func waitingMenuRun() *workflow.WorkflowRun {
	return &workflow.WorkflowRun{
		ID: "run-woken", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "entry-1",
		Status: workflow.RunStatusWaiting, WaitReason: workflow.WaitReasonReply, CurrentNodeID: "menu", State: workflow.NewRunState(),
	}
}

func messageEvent() workflow.TriggerEvent {
	return workflow.TriggerEvent{
		WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "telegram", TriggerType: workflow.TriggerMessageReceived,
		Data: map[string]interface{}{"message": "oi", "account_workflow_id": "wf-1"},
	}
}

func TestAFailedWaitingRunLookupIsLoggedCountedAndReturned(t *testing.T) {
	spy := &metricsSpy{}
	engine := NewRunEngine(&recordingRunRepo{}, stubRunLogRepo{}, NewNodeExecutorRegistry())
	engine.SetMetrics(spy)
	waiters := NewReplyWaiters(stubWorkflowRepo{}, lookupFailingRuns{&recordingRunRepo{}}, engine)

	run, err := waiters.WaitingForReply(messageEvent())

	if err == nil || run != nil {
		t.Fatalf("run = %+v, err = %v; the caller must learn the lookup failed", run, err)
	}
	if spy.lookupFailures != 1 {
		t.Fatalf("lookup failures counted = %d, want 1", spy.lookupFailures)
	}
}

func TestAMessageRoutedToAWaitingRunIsNotWokenOrStartedAgainByTheEvaluator(t *testing.T) {
	wf := replyMenuWorkflow()
	runs := &recordingRunRepo{waitingReply: waitingMenuRun()}
	te := NewTriggerEvaluator(stubWorkflowRepo{byID: map[string]*workflow.Workflow{wf.ID: wf}, active: []*workflow.Workflow{wf}}, runs,
		NewRunEngine(runs, stubRunLogRepo{}, NewNodeExecutorRegistry()), nil)

	event := messageEvent()
	event.Reply = &workflow.ReplyRouting{RunID: "run-woken", WorkflowID: wf.ID}
	te.Evaluate(event)

	if runs.lookups != 0 {
		t.Fatalf("the evaluator looked the waiting run up %d time(s) for a message already routed", runs.lookups)
	}
	if runs.updates != 0 || runs.created != 0 {
		t.Fatalf("updates = %d, created = %d; the routed run was woken or restarted twice", runs.updates, runs.created)
	}
}

func TestAMessageRoutedToNoRunSkipsTheLookupButStillStartsTriggers(t *testing.T) {
	wf := replyMenuWorkflow()
	runs := &recordingRunRepo{waitingReply: waitingMenuRun()}
	te := NewTriggerEvaluator(stubWorkflowRepo{byID: map[string]*workflow.Workflow{wf.ID: wf}, active: []*workflow.Workflow{wf}}, runs,
		NewRunEngine(runs, stubRunLogRepo{}, NewNodeExecutorRegistry()), nil)

	event := messageEvent()
	event.Reply = &workflow.ReplyRouting{}
	te.Evaluate(event)

	if runs.lookups != 0 {
		t.Fatalf("looked up %d time(s); the router already found no waiting run", runs.lookups)
	}
	if runs.created != 1 {
		t.Fatalf("created %d runs, want the message trigger to start one", runs.created)
	}
}

func TestAnUnroutedMessageWhoseLookupFailsStartsNothing(t *testing.T) {
	wf := replyMenuWorkflow()
	inner := &recordingRunRepo{}
	runs := lookupFailingRuns{inner}
	te := NewTriggerEvaluator(stubWorkflowRepo{byID: map[string]*workflow.Workflow{wf.ID: wf}, active: []*workflow.Workflow{wf}}, runs,
		NewRunEngine(runs, stubRunLogRepo{}, NewNodeExecutorRegistry()), nil)

	te.Evaluate(messageEvent())

	if inner.created != 0 {
		t.Fatalf("created %d runs although the waiting run could not be read", inner.created)
	}
}
