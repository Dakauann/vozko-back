package workflow_usecase

import (
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

func TestWorkspaceSlot(t *testing.T) {
	if !tryAcquireWorkspaceSlot(nil, "ws1") {
		t.Fatal("nil state must fail-open to true")
	}
	releaseWorkspaceSlot(nil, "ws1") // must not panic

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
	releaseWorkspaceSlot(failing, "ws1") // must not panic on Decr error
}

func TestExecuteLockedLogsEngineError(t *testing.T) {
	runRepo := NewMockWorkflowRunRepository()
	runRepo.UpdateErr = errorString("update failed")
	logRepo := NewMockWorkflowRunLogRepository()
	engine := NewRunEngine(runRepo, logRepo, NewNodeExecutorRegistry())

	w := webhookWorkflow()
	run := newTriggeredRun(w, w.Graph.TriggerNodeByType(workflow.TriggerWebhook), webhookEvent())
	executeLocked(engine, run, w) // Update error surfaces from Execute; branch must be handled, not panic
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
	launcher.Launch(run, w) // async goroutine; just assert it does not panic

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if stored, _ := runRepo.FindByID(run.ID); stored != nil && stored.Status == workflow.RunStatusCompleted {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("async launched run did not complete in time")
}

type fakeRunLocker struct{ acquired bool }

func (l *fakeRunLocker) TryLock(runID string, ttl time.Duration) (bool, error) {
	return l.acquired, nil
}
func (l *fakeRunLocker) Unlock(runID string) error { return nil }

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
