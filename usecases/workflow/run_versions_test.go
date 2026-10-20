package workflow_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/workflow"
)

func waitingGraph(waitID, endID string) workflow.Graph {
	return workflow.Graph{
		Nodes: []workflow.Node{
			{ID: "t", Type: workflow.NodeTypeTriggerMessageReceived, Config: map[string]interface{}{}},
			{ID: waitID, Type: workflow.NodeTypeWaitDuration, Config: map[string]interface{}{"seconds": 3600}},
			{ID: endID, Type: workflow.NodeTypeEnd, Config: map[string]interface{}{}},
		},
		Edges: []workflow.Edge{
			{Source: "t", Target: waitID},
			{Source: waitID, Target: endID, Label: "completed"},
			{Source: waitID, Target: endID, Label: "message_received"},
		},
	}
}

func republished(t *testing.T, repo *MockWorkflowRepository, g workflow.Graph) {
	t.Helper()
	if _, err := NewUpdateWorkflowUseCase(repo).Execute(context.Background(), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: g}, revisionOf(repo, "wf-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := activation(repo).Publish(publisherCtx(), "wf-1"); err != nil {
		t.Fatal(err)
	}
}

func automationOnEngine(runs workflow.WorkflowRunRepository) *RunEngine {
	engine := NewRunEngine(runs, stubRunLogRepo{}, NewNodeExecutorRegistry())
	engine.SetAutomationGate(SandboxAutomationGate{})
	return engine
}

func versionedWorkflow(t *testing.T) *MockWorkflowRepository {
	t.Helper()
	repo := NewMockWorkflowRepository()
	storedWorkflow(t, repo, workflow.WorkflowStatusActive, waitingGraph("wait-v1", "end-v1"))
	republished(t, repo, waitingGraph("wait-v2", "end-v2"))
	if _, err := NewUpdateWorkflowUseCase(repo).Execute(context.Background(), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: waitingGraph("wait-draft", "end-draft")}, revisionOf(repo, "wf-1")); err != nil {
		t.Fatal(err)
	}
	return repo
}

func parkedOn(nodeID string, version *int) *workflow.WorkflowRun {
	return &workflow.WorkflowRun{
		ID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws1", EntryID: "entry-1", EntryType: "whatsapp",
		Status: workflow.RunStatusWaiting, WaitReason: workflow.WaitReasonDuration, TriggerNodeID: "t", CurrentNodeID: nodeID,
		State: workflow.NewRunState(), WorkflowVersion: version,
	}
}

func TestAResumedRunKeepsTheGraphItStartedOn(t *testing.T) {
	repo := versionedWorkflow(t)
	runs := NewMockWorkflowRunRepository()
	one := 1
	run := parkedOn("wait-v1", &one)
	_ = runs.Create(run)

	if err := resumeRunFromCurrent(repo, runs, automationOnEngine(runs), run); err != nil {
		t.Fatalf("a run pinned to version 1 must find its nodes after version 2 was published: %v", err)
	}
	if run.Status != workflow.RunStatusCompleted {
		t.Fatalf("status = %s error = %q", run.Status, run.Error)
	}
}

func TestARunInFlightBeforeVersionsFollowsThePublishedGraph(t *testing.T) {
	repo := versionedWorkflow(t)
	runs := NewMockWorkflowRunRepository()
	run := parkedOn("wait-v2", nil)
	_ = runs.Create(run)

	if err := resumeRunFromCurrent(repo, runs, automationOnEngine(runs), run); err != nil || run.Status != workflow.RunStatusCompleted {
		t.Fatalf("status = %s err = %v, want the published version, never the draft", run.Status, err)
	}
}

func TestAPinnedRunWhoseVersionIsGoneFailsInsteadOfRunningTheDraft(t *testing.T) {
	repo := versionedWorkflow(t)
	runs := NewMockWorkflowRunRepository()
	missing := 9
	run := parkedOn("wait-draft", &missing)
	_ = runs.Create(run)

	if err := resumeRunFromCurrent(repo, runs, automationOnEngine(runs), run); err != nil || run.Status != workflow.RunStatusError {
		t.Fatalf("status = %s err = %v, want the run failed and the wake settled", run.Status, err)
	}
}

func TestATransientVersionReadOnResumeLeavesTheRunWaitingForTheRetry(t *testing.T) {
	repo := versionedWorkflow(t)
	runs := NewMockWorkflowRunRepository()
	one := 1
	run := parkedOn("wait-v1", &one)
	_ = runs.Create(run)
	blip := errors.New("connection reset")
	repo.VersionErr = blip

	err := resumeRunFromCurrent(repo, runs, automationOnEngine(runs), run)
	if !errors.Is(err, blip) {
		t.Fatalf("err = %v, want the read error so the wake is retried", err)
	}
	stored, _ := runs.FindByID("run-1")
	if stored.Status != workflow.RunStatusWaiting || stored.Error != "" {
		t.Fatalf("a database blip must not fail the run: %+v", stored)
	}
}

func replyParkedOn(nodeID string, version *int) *workflow.WorkflowRun {
	run := parkedOn(nodeID, version)
	run.WaitReason = workflow.WaitReasonReply
	return run
}

func TestAReplyWakeFailsTheRunOnlyWhenItsVersionIsGone(t *testing.T) {
	cases := []struct {
		name       string
		version    int
		versionErr error
		want       workflow.RunStatus
	}{
		{"the pinned version was deleted", 9, nil, workflow.RunStatusError},
		{"the version read failed for a moment", 1, errors.New("connection reset"), workflow.RunStatusWaiting},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := versionedWorkflow(t)
			runs := NewMockWorkflowRunRepository()
			version := tc.version
			run := replyParkedOn("wait-v1", &version)
			_ = runs.Create(run)
			repo.VersionErr = tc.versionErr

			waiters := NewReplyWaiters(repo, runs, automationOnEngine(runs))
			if waiters.WakeForReply(run, workflow.TriggerEvent{EntryID: "entry-1", Data: map[string]interface{}{"message": "oi"}}) {
				t.Fatal("a run whose graph cannot be read is never woken")
			}
			stored, _ := runs.FindByID("run-1")
			if stored.Status != tc.want {
				t.Fatalf("status = %s error = %q, want %s", stored.Status, stored.Error, tc.want)
			}
		})
	}
}

func TestAFormAnswerOnARunWhoseVersionIsGoneFailsTheRun(t *testing.T) {
	repo := versionedWorkflow(t)
	runs := NewMockWorkflowRunRepository()
	missing := 9
	run := parkedOn("wait-v1", &missing)
	run.WaitReason = workflow.WaitReasonForm
	_ = runs.Create(run)

	if err := NewFormRunResumer(repo, runs, automationOnEngine(runs)).ResumeForm(context.Background(), "run-1", formAnswers()); err != nil {
		t.Fatal(err)
	}
	stored, _ := runs.FindByID("run-1")
	if stored.Status != workflow.RunStatusError {
		t.Fatalf("status = %s, want error", stored.Status)
	}
}

func TestATriggeredRunRecordsThePublishedVersion(t *testing.T) {
	repo := versionedWorkflow(t)
	runs := NewMockWorkflowRunRepository()
	te := NewTriggerEvaluator(repo, runs, automationOnEngine(runs), nil).(*triggerEvaluator)

	te.Evaluate(workflow.TriggerEvent{WorkspaceID: "ws1", EntryID: "entry-1", EntryType: "whatsapp", TriggerType: workflow.TriggerMessageReceived, Data: map[string]interface{}{"message": "oi"}})

	created := runs.runs
	if len(created) != 1 {
		t.Fatalf("runs = %d, want 1", len(created))
	}
	for _, run := range created {
		if run.WorkflowVersion == nil || *run.WorkflowVersion != 2 || run.CurrentNodeID != "wait-v2" {
			t.Fatalf("run = version %v at %s, want version 2 parked on its wait node", run.WorkflowVersion, run.CurrentNodeID)
		}
	}
}

func TestAMessageWakesAWaitOnTheVersionTheRunStartedOn(t *testing.T) {
	repo := versionedWorkflow(t)
	runs := NewMockWorkflowRunRepository()
	one := 1
	run := parkedOn("wait-v1", &one)
	_ = runs.Create(run)
	te := NewTriggerEvaluator(repo, runs, automationOnEngine(runs), nil).(*triggerEvaluator)

	te.Evaluate(workflow.TriggerEvent{WorkspaceID: "ws1", EntryID: "entry-1", EntryType: "whatsapp", TriggerType: workflow.TriggerMessageReceived, Data: map[string]interface{}{"message": "oi"}})

	woken, _ := runs.FindByID("run-1")
	if woken.Status != workflow.RunStatusCompleted || woken.State.GetString("_wait_outcome") != "message_received" {
		t.Fatalf("status = %s outcome = %q, want the version 1 wait to take its message edge", woken.Status, woken.State.GetString("_wait_outcome"))
	}
}

func TestAConversationStartUsesThePublishedTriggers(t *testing.T) {
	h := newConversationStartHarness()
	h.wfRepo = NewMockWorkflowRepository()
	storedWorkflow(t, h.wfRepo, workflow.WorkflowStatusActive, waitingGraph("wait-v1", "end-v1"))
	draft := waitingGraph("wait-v1", "end-v1")
	draft.Nodes[0] = workflow.Node{ID: "t", Type: workflow.NodeTypeTriggerAgentCall, Config: map[string]interface{}{}}
	if _, err := NewUpdateWorkflowUseCase(h.wfRepo).Execute(context.Background(), "wf-1", &workflow.Workflow{Name: "Fluxo", Graph: draft}, revisionOf(h.wfRepo, "wf-1")); err != nil {
		t.Fatal(err)
	}
	starter, err := NewConversationRunStarter(h.wfRepo, h.runRepo, h.launcher, h.state)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := starter.StartForConversation(ConversationStart{WorkspaceID: "ws1", WorkflowID: "wf-1", EntryID: "e1", EntryType: "whatsapp", TriggerType: workflow.TriggerAgentCall}); !errors.Is(err, workflow.ErrTriggerNotInWorkflow) {
		t.Fatalf("err = %v, want the unpublished agent-call trigger to be unavailable", err)
	}
	started, err := starter.StartForConversation(ConversationStart{WorkspaceID: "ws1", WorkflowID: "wf-1", EntryID: "e1", EntryType: "whatsapp"})
	if err != nil || started.RunID == "" {
		t.Fatalf("started = %+v err = %v", started, err)
	}
	if got := h.launcher.launched[0].WorkflowVersion; got == nil || *got != 1 {
		t.Fatalf("version = %v, want 1", got)
	}
}

func TestAVoiceCallRecordsThePublishedVersion(t *testing.T) {
	repo := NewMockWorkflowRepository()
	w := voiceWorkflow("wf-1", "trunk-1", "greeting")
	w.Status = workflow.WorkflowStatusActive
	w.Normalize()
	_ = repo.Create(w)
	runs := NewMockWorkflowRunRepository()
	flows := NewInboundVoiceFlows(repo, runs, automationOnEngine(runs))

	flow, err := flows.FlowFor("ws1", "trunk-1")
	if err != nil || flow == nil {
		t.Fatalf("flow = %v err = %v", flow, err)
	}
	run := newVoiceRun(flow, flow.Graph.TriggerNodeByType(workflow.TriggerCallReceived), workflow.InboundVoiceCall{WorkspaceID: "ws1", CallID: "c1"})
	if run.WorkflowVersion == nil || *run.WorkflowVersion != 1 {
		t.Fatalf("version = %v, want 1", run.WorkflowVersion)
	}
}

func TestCampaignChecksReadThePublishedGraph(t *testing.T) {
	repo := versionedWorkflow(t)
	got, err := NewGetPublishedWorkflowUseCase(repo).Execute("wf-1")
	if err != nil || got.Graph.FindNode("wait-v2") == nil {
		t.Fatalf("got = %+v err = %v, want the published version 2", got, err)
	}
	if _, err := NewGetPublishedWorkflowUseCase(repo).Execute("nope"); !errors.Is(err, workflow.ErrWorkflowNotFound) {
		t.Fatalf("err = %v", err)
	}
}
