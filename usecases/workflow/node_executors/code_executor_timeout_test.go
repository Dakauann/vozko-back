package node_executors

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/workflow"
	"vozko/usecases/workflow/scriptvm"
)

func runCode(t *testing.T, code string, timeoutMs float64) (time.Duration, error) {
	t.Helper()
	state := workflow.RunState{}
	ctx := &workflow.NodeContext{
		Run:   &workflow.WorkflowRun{},
		Node:  &workflow.Node{ID: "n1", Type: workflow.NodeTypeActionCode, Config: map[string]interface{}{"code": code, "timeout_ms": timeoutMs}},
		State: &state,
		Trace: &workflow.StepTrace{},
	}
	started := time.Now()
	_, err := NewCodeExecutor(nil).Execute(ctx)
	return time.Since(started), err
}

func TestTheCodeNodeHonoursATimeoutLongerThanTheDefault(t *testing.T) {
	if _, err := runCode(t, "sleep(6000); return {ok: true}", 8000); err != nil {
		t.Fatalf("a 6 s script with an 8 s timeout failed: %v", err)
	}
}

func TestTheCodeNodeStopsAtItsOwnTimeout(t *testing.T) {
	took, err := runCode(t, "sleep(3000); return {ok: true}", 500)
	if !errors.Is(err, scriptvm.ErrTimeout) || took > 2*time.Second {
		t.Fatalf("err = %v after %s, want a timeout near 500 ms", err, took)
	}
}

func TestTheCodeNodeDeclaresItsTimeoutCeiling(t *testing.T) {
	for _, field := range NewCodeExecutor(nil).(*codeExecutor).Definition().ConfigSchema {
		if field.Key == "timeout_ms" {
			if field.Max == nil || *field.Max != float64(workflow.CodeNodeMaxTimeout.Milliseconds()) {
				t.Fatalf("timeout_ms max = %v", field.Max)
			}
			return
		}
	}
	t.Fatal("timeout_ms field missing")
}
