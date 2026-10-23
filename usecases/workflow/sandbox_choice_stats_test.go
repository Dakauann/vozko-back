package workflow_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/choicestats"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

type spyChoiceRecorder struct {
	sends  []choicestats.Send
	clicks []choicestats.Click
}

func (s *spyChoiceRecorder) RecordSend(_ context.Context, send choicestats.Send) {
	s.sends = append(s.sends, send)
}

func (s *spyChoiceRecorder) RecordClick(_ context.Context, click choicestats.Click) {
	s.clicks = append(s.clicks, click)
}

func TestTheSandboxNeverRecordsChoiceStatsEvenWhenTheRecorderIsWired(t *testing.T) {
	spy := &spyChoiceRecorder{}
	box := newSandboxRun(ExecutorDeps{ChoiceStats: spy})
	node := workflow.Node{ID: "ask", Type: workflow.NodeTypeActionSendInteractive, Config: map[string]interface{}{
		"interactive_type": "buttons",
		"body":             "Quer continuar?",
		"buttons":          `[{"Type":"reply","ID":"sim","Title":"Sim"},{"Type":"reply","ID":"nao","Title":"Não"}]`,
	}}
	g := workflow.Graph{
		Nodes: []workflow.Node{node, {ID: "end", Type: workflow.NodeTypeEnd}},
		Edges: []workflow.Edge{{Source: "ask", Target: "end", Label: "sim"}, {Source: "ask", Target: "end", Label: "nao"}},
	}
	run := &workflow.WorkflowRun{ID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws1", EntryID: "sim-entry", EntryType: "whatsapp", CurrentNodeID: "ask", State: workflow.NewRunState()}
	executor, ok := box.registry.Get(node.Type)
	if !ok {
		t.Fatal("no interactive executor in the sandbox")
	}
	result, err := executor.Execute(&workflow.NodeContext{Run: run, Node: &g.Nodes[0], Graph: &g, Workflow: &workflow.Workflow{ID: "wf-1", WorkspaceID: "ws1", Graph: g}, State: &run.State, Trace: &workflow.StepTrace{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Wait == nil || len(box.sent()) != 1 {
		t.Fatalf("the sandbox must still send and wait like production, result = %+v", result)
	}
	if len(spy.sends) != 0 || run.State.GetString(workflow.StateKeyChoicePrompt) != "" {
		t.Fatalf("a test run counted toward the click-through: %+v", spy.sends)
	}

	if newSandboxEngine(nil, nil, NewNodeExecutorRegistry(), nil).choices != nil {
		t.Fatal("the sandbox engine has no recorder, so its replies are never counted")
	}
}

func TestProductionExecutorsRefuseToStartWithoutTheChoiceRecorder(t *testing.T) {
	if err := RegisterProductionExecutors(NewNodeExecutorRegistry(), ExecutorDeps{ConversationEvents: silentLines{}, Forms: node_executors.NewFormSenderRelay()}); !errors.Is(err, ErrChoiceRecorderRequired) {
		t.Fatalf("got %v, want ErrChoiceRecorderRequired", err)
	}
}
