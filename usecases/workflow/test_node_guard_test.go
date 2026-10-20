package workflow_usecase

import (
	"context"
	"testing"
	"time"

	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

func agentToolLoopTester(t *testing.T, laps int) TestNodeUseCase {
	t.Helper()
	wf := createTestWorkflow("wf-tools", "ws1", []workflow.Node{
		{ID: "t", Type: workflow.NodeTypeTriggerMessageReceived},
		{ID: "agent", Type: workflow.NodeTypeActionAIAgent, Config: map[string]interface{}{"source": "inline", "prompt": "Use as ferramentas"}},
		{ID: "tool", Type: workflow.NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "consulta", "value": "ok"}},
		{ID: "target", Type: workflow.NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "fim", "value": "{{node.tool.value}}"}},
		{ID: "end", Type: workflow.NodeTypeEnd},
	}, []workflow.Edge{
		{Source: "t", Target: "agent"},
		{Source: "agent", Target: "tool", Label: "consultar"},
		{Source: "tool", Target: "agent"},
		{Source: "agent", Target: "target", Label: "pronto"},
		{Source: "target", Target: "end"},
	})
	wf.Type = workflow.WorkflowTypeMessages
	repo := NewMockWorkflowRepository()
	_ = repo.Create(wf)
	calls := 0
	registry := NewNodeExecutorRegistry()
	registry.Register(workflow.NodeTypeActionAIAgent, funcExecutor{
		def: workflow.NodeDefinition{Type: workflow.NodeTypeActionAIAgent, Label: "Agente IA", Scopes: []workflow.NodeScope{workflow.NodeScopeWhatsApp}},
		run: func(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
			calls++
			if calls <= laps {
				return &workflow.NodeResult{Output: map[string]interface{}{"tool": "consultar"}, NextNodeID: "tool"}, nil
			}
			return &workflow.NodeResult{Output: map[string]interface{}{"response": "pronto"}, NextNodeID: "target"}, nil
		},
	})
	registry.Register(workflow.NodeTypeActionSetVariable, node_executors.NewSetVariableExecutor())
	return NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Executors: fixedExecutors(registry), Timeout: 2 * time.Second})
}

func TestTheNodeTestAllowsAnAgentToolCycleLikeTheEngine(t *testing.T) {
	out, err := agentToolLoopTester(t, 6).Execute(context.Background(), TestNodeInput{WorkflowID: "wf-tools", WorkspaceID: "ws1", NodeID: "target", TriggerVars: map[string]interface{}{"message": "oi"}})
	if err != nil || !out.Success || out.PathStop != nil || len(out.UpstreamSteps) < 13 {
		t.Fatalf("out = %+v err = %v; six tool laps are fine in production, the test must reach the target too", out, err)
	}
}

func TestTheNodeTestStopsAtTheDurableStepBoundLikeTheEngine(t *testing.T) {
	out, err := agentToolLoopTester(t, 0).Execute(context.Background(), TestNodeInput{
		WorkflowID: "wf-tools", WorkspaceID: "ws1", NodeID: "target", TriggerVars: map[string]interface{}{"message": "oi", workflow.StateKeyDurableSteps: workflow.MaxDurableExecutionsPerRun},
	})
	if err != nil || out.Success || out.PathStop == nil || out.PathStop.Reason != PathStopLoop {
		t.Fatalf("out = %+v err = %v, want the durable step guard", out, err)
	}
}
