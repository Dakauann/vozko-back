package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workflow"
)

func agentToolCycle() workflow.Graph {
	return workflow.Graph{
		Nodes: []workflow.Node{
			{ID: "t", Type: workflow.NodeTypeTriggerMessageReceived},
			{ID: "ai", Type: workflow.NodeTypeActionAIAgent},
			{ID: "tool", Type: workflow.NodeTypeActionHTTPRequest},
			{ID: "plain", Type: workflow.NodeTypeActionSetVariable},
		},
		Edges: []workflow.Edge{
			{Source: "t", Target: "ai"}, {Source: "ai", Target: "tool"}, {Source: "tool", Target: "ai"}, {Source: "ai", Target: "plain"}, {Source: "plain", Target: "plain"},
		},
	}
}

func admitUntilRefused(guard *stepGuard, node *workflow.Node) (int, error) {
	for visit := 1; visit <= workflow.MaxExecutionsPerRun+1; visit++ {
		if err := guard.admit(node); err != nil {
			return visit, err
		}
	}
	return 0, nil
}

func TestTheStepGuardIsTheEnginesLoopRuleForEveryCaller(t *testing.T) {
	g := agentToolCycle()
	cases := []struct {
		name string
		node string
		want int
	}{
		{"a plain node revisited without waiting", "plain", 4},
		{"a node in an agent tool cycle", "tool", maxAgentCycleRevisits + 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := workflow.NewRunState()
			guard := newStepGuard(&g, &state)
			refusedAt, err := admitUntilRefused(guard, g.FindNode(tc.node))
			if refusedAt != tc.want || !errors.Is(err, errLoopDetected) {
				t.Fatalf("refused at visit %d with %v, want visit %d with a loop", refusedAt, err, tc.want)
			}
		})
	}
}

func TestTheStepGuardStopsAtTheDurableStepBoundAcrossResumes(t *testing.T) {
	g := agentToolCycle()
	state := workflow.NewRunState()
	state.Set(workflow.StateKeyDurableSteps, workflow.MaxDurableExecutionsPerRun)
	guard := newStepGuard(&g, &state)
	if err := guard.admit(g.FindNode("ai")); !errors.Is(err, workflow.ErrDurableExecutionsReached) {
		t.Fatalf("err = %v, want the durable step bound", err)
	}
}

func TestTheStepGuardCountsExecutionsAndForgetsVisitsAfterAWait(t *testing.T) {
	g := agentToolCycle()
	state := workflow.NewRunState()
	guard := newStepGuard(&g, &state)
	plain := g.FindNode("plain")
	for i := 0; i < 3; i++ {
		if err := guard.admit(plain); err != nil {
			t.Fatal(err)
		}
	}
	guard.waited()
	if err := guard.admit(plain); err != nil {
		t.Fatalf("a wait resets the revisit count: %v", err)
	}
	for i := 0; i < workflow.MaxExecutionsPerRun; i++ {
		guard.executed()
	}
	if err := guard.full(); !errors.Is(err, workflow.ErrMaxExecutionsReached) {
		t.Fatalf("err = %v", err)
	}
	if got := state.GetInt(workflow.StateKeyDurableSteps); got != 4 {
		t.Fatalf("durable steps = %d, each admitted step is counted in the run state", got)
	}
}
