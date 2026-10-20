package workflow_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/workflow"
)

func webhookEvent() workflow.TriggerEvent {
	return workflow.TriggerEvent{
		WorkspaceID: "ws1",
		EntryID:     "entry1",
		EntryType:   "whatsapp",
		TriggerType: workflow.TriggerWebhook,
		Data:        map[string]interface{}{"webhook": map[string]interface{}{"body": "x"}},
	}
}

func TestNewTriggeredRun(t *testing.T) {
	w := webhookWorkflow()
	trigger := w.Graph.TriggerNodeByType(workflow.TriggerWebhook)
	run := newTriggeredRun(w, trigger, webhookEvent())

	if run.TriggerNodeID != "twh" || run.CurrentNodeID != "twh" {
		t.Fatalf("run must start at trigger, got trigger=%s current=%s", run.TriggerNodeID, run.CurrentNodeID)
	}
	if run.WorkflowID != "wf1" || run.WorkspaceID != "ws1" || run.EntryID != "entry1" || run.EntryType != "whatsapp" {
		t.Fatalf("run fields mismatch: %+v", run)
	}
	if run.Status != workflow.RunStatusRunning {
		t.Fatalf("expected running, got %s", run.Status)
	}
	if _, ok := run.State.Get("webhook"); !ok {
		t.Fatal("expected event data seeded into state")
	}
}

func TestEveryRunConstructorStoresWhatTheRunStartedWith(t *testing.T) {
	w := webhookWorkflow()
	triggered := newTriggeredRun(w, w.Graph.TriggerNodeByType(workflow.TriggerWebhook), webhookEvent())
	body, _ := triggered.TriggerData["webhook"].(map[string]interface{})
	if body["body"] != "x" {
		t.Fatalf("triggered run trigger data = %v, want the webhook body so the run can be reproduced", triggered.TriggerData)
	}

	voice := newVoiceRun(w, &w.Graph.Nodes[0], workflow.InboundVoiceCall{WorkspaceID: "ws1", CallID: "c-1", CallerNumber: "5511999990000"})
	if voice.TriggerData["call_id"] != "c-1" {
		t.Fatalf("voice run trigger data = %v", voice.TriggerData)
	}

	h := startRunFixture(t, seesEverything{})
	in := manualStart("whatsapp")
	in.Variables = map[string]interface{}{"nome": "Ana"}
	if _, err := h.uc.Execute(in); err != nil {
		t.Fatal(err)
	}
	if created := h.created(t); created.TriggerData["nome"] != "Ana" {
		t.Fatalf("manual run trigger data = %v", created.TriggerData)
	}
}

func TestWorkspaceSlot(t *testing.T) {
	if !tryAcquireWorkspaceSlot(nil, "ws1") {
		t.Fatal("nil state must fail-open to true")
	}
	releaseWorkspaceSlot(nil, "ws1")

	deny := &fakeSharedState{allow: false}
	if tryAcquireWorkspaceSlot(deny, "ws1") {
		t.Fatal("deny state must return false")
	}

	allow := &fakeSharedState{allow: true}
	if !tryAcquireWorkspaceSlot(allow, "ws1") {
		t.Fatal("allow state must return true")
	}
	releaseWorkspaceSlot(allow, "ws1")
	if allow.decrs != 1 {
		t.Fatalf("release must decrement, got %d", allow.decrs)
	}
}

func TestWorkspaceSlotFailOpenOnError(t *testing.T) {
	failing := &fakeSharedState{incrErr: errorString("redis down")}
	if !tryAcquireWorkspaceSlot(failing, "ws1") {
		t.Fatal("TryIncr error must fail-open to true")
	}
	failing.decrErr = errorString("redis down")
	releaseWorkspaceSlot(failing, "ws1")
}

func TestExecuteLockedLogsEngineError(t *testing.T) {
	runRepo := NewMockWorkflowRunRepository()
	runRepo.UpdateErr = errorString("update failed")
	logRepo := NewMockWorkflowRunLogRepository()
	engine := NewRunEngine(runRepo, logRepo, NewNodeExecutorRegistry())

	w := webhookWorkflow()
	run := newTriggeredRun(w, w.Graph.TriggerNodeByType(workflow.TriggerWebhook), webhookEvent())
	executeLocked(engine, run, w)
}

func TestBuildWebhookVars(t *testing.T) {
	jsonVars := buildWebhookVars(WebhookRequest{Method: "POST"}, map[string]interface{}{"k": "v"})
	if body, _ := jsonVars["body"].(map[string]interface{}); body["k"] != "v" {
		t.Fatalf("expected parsed body map, got %v", jsonVars["body"])
	}

	rawVars := buildWebhookVars(WebhookRequest{Method: "POST", RawBody: []byte("plain")}, nil)
	if rawVars["body"] != "plain" {
		t.Fatalf("expected raw string body when not JSON, got %v", rawVars["body"])
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }

func TestEngineRunLauncher_RunsAndReleasesSlot(t *testing.T) {
	runRepo := NewMockWorkflowRunRepository()
	logRepo := NewMockWorkflowRunLogRepository()
	engine := NewRunEngine(runRepo, logRepo, NewNodeExecutorRegistry())
	state := &fakeSharedState{allow: true}

	launcher := &engineRunLauncher{
		engine:      engine,
		sharedState: state,
		dispatch:    func(fn func()) { fn() },
	}

	w := webhookWorkflow()
	trigger := w.Graph.TriggerNodeByType(workflow.TriggerWebhook)
	run := newTriggeredRun(w, trigger, webhookEvent())

	launcher.Launch(run, w)

	if run.Status != workflow.RunStatusCompleted {
		t.Fatalf("expected completed run (trigger->end), got %s err=%s", run.Status, run.Error)
	}
	if state.decrs != 1 {
		t.Fatalf("launcher must release the slot, decrs=%d", state.decrs)
	}
}

func TestNewEngineRunLauncherDefaultDispatch(t *testing.T) {
	runRepo := NewMockWorkflowRunRepository()
	logRepo := NewMockWorkflowRunLogRepository()
	engine := NewRunEngine(runRepo, logRepo, NewNodeExecutorRegistry())

	launcher := NewEngineRunLauncher(engine, &fakeSharedState{allow: true})
	w := webhookWorkflow()
	run := newTriggeredRun(w, w.Graph.TriggerNodeByType(workflow.TriggerWebhook), webhookEvent())
	launcher.Launch(run, w)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if stored, _ := runRepo.FindByID(run.ID); stored != nil && stored.Status == workflow.RunStatusCompleted {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("async launched run did not complete in time")
}

type fakeRunLocker struct {
	acquired bool
	err      error
}

func (l *fakeRunLocker) TryLock(runID string, ttl time.Duration) (bool, error) {
	return l.acquired, l.err
}
func (l *fakeRunLocker) Unlock(runID string) error { return nil }

func TestAnUnreadableRunLockLeavesTheRunForALaterRetry(t *testing.T) {
	engine := NewRunEngine(NewMockWorkflowRunRepository(), NewMockWorkflowRunLogRepository(), NewNodeExecutorRegistry())
	engine.SetRunLocker(&fakeRunLocker{acquired: true, err: errors.New("redis down")})
	if engine.TryLockRun("run-1") {
		t.Fatal("a lock store outage must not let two workers run the same step")
	}
}

func TestExecuteLockedSkipsWhenLocked(t *testing.T) {
	runRepo := NewMockWorkflowRunRepository()
	logRepo := NewMockWorkflowRunLogRepository()
	engine := NewRunEngine(runRepo, logRepo, NewNodeExecutorRegistry())
	engine.SetRunLocker(&fakeRunLocker{acquired: false})

	w := webhookWorkflow()
	run := newTriggeredRun(w, w.Graph.TriggerNodeByType(workflow.TriggerWebhook), webhookEvent())
	executeLocked(engine, run, w)

	if run.Status == workflow.RunStatusCompleted {
		t.Fatal("run must not advance when the lock is held elsewhere")
	}
}
