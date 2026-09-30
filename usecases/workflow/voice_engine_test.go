package workflow_usecase

import (
	"context"
	"testing"
	"time"

	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

type scriptedCaller struct {
	keys   []rune
	plays  int
	hangUp bool
}

func (c *scriptedCaller) Play(_ []byte, _ bool) (bool, error) {
	c.plays++
	return false, nil
}

func (c *scriptedCaller) NextKey(_ time.Duration) (rune, bool, error) {
	if c.hangUp && len(c.keys) == 0 {
		return 0, false, workflow.ErrCallEnded
	}
	if len(c.keys) == 0 {
		return 0, false, nil
	}
	key := c.keys[0]
	c.keys = c.keys[1:]
	return key, true, nil
}

type silentAudio struct{}

func (silentAudio) LoadPCM(context.Context, string, string) ([]byte, error) {
	return make([]byte, 320), nil
}

func voiceEngine(runs *MockWorkflowRunRepository) *RunEngine {
	registry := NewNodeExecutorRegistry()
	registry.Register(workflow.NodeTypeActionPlayAudio, node_executors.NewPlayAudioExecutor(silentAudio{}))
	registry.Register(workflow.NodeTypeWaitDTMF, node_executors.NewWaitDTMFExecutor())
	return NewRunEngine(runs, NewMockWorkflowRunLogRepository(), registry)
}

func voiceRun(w *workflow.Workflow) *workflow.WorkflowRun {
	return &workflow.WorkflowRun{
		ID: "run-1", WorkflowID: w.ID, WorkspaceID: w.WorkspaceID,
		Status: workflow.RunStatusRunning, TriggerNodeID: "t", CurrentNodeID: "t", State: workflow.NewRunState(),
	}
}

func TestACallerCanReplayTheMenuWithoutTrippingTheLoopGuard(t *testing.T) {
	runs := NewMockWorkflowRunRepository()
	w := voiceWorkflow("wf-1", "trunk-1", "greeting")
	run := voiceRun(w)
	_ = runs.Create(run)
	caller := &scriptedCaller{keys: []rune{'9', '9', '9', '9', '9', '1'}}

	if err := voiceEngine(runs).ExecuteWithRuntime(run, w, caller); err != nil {
		t.Fatalf("ExecuteWithRuntime: %v", err)
	}
	if run.Status != workflow.RunStatusCompleted {
		t.Fatalf("status = %s (%s), want completed", run.Status, run.Error)
	}
	if caller.plays != 6 {
		t.Fatalf("menu played %d times, want 6", caller.plays)
	}
}

func TestAHangUpMidMenuCancelsTheRun(t *testing.T) {
	runs := NewMockWorkflowRunRepository()
	w := voiceWorkflow("wf-1", "trunk-1", "greeting")
	run := voiceRun(w)
	_ = runs.Create(run)

	if err := voiceEngine(runs).ExecuteWithRuntime(run, w, &scriptedCaller{hangUp: true}); err != nil {
		t.Fatalf("ExecuteWithRuntime: %v", err)
	}
	if run.Status != workflow.RunStatusCancelled {
		t.Fatalf("status = %s, want cancelled", run.Status)
	}
}
