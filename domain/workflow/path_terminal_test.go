package workflow

import (
	"errors"
	"testing"
)

func agentCallGraph(nodes []Node, edges []Edge) *Graph {
	all := append([]Node{{ID: "t", Type: NodeTypeTriggerAgentCall, Config: map[string]interface{}{}}}, nodes...)
	return &Graph{Nodes: all, Edges: edges}
}

func TestAReplyToTheAgentEndsAPathWithoutAnEndNode(t *testing.T) {
	g := agentCallGraph(
		[]Node{{ID: "r", Type: NodeTypeActionReplyToAgent, Config: map[string]interface{}{}}},
		[]Edge{{Source: "t", Target: "r"}},
	)
	if problems := GraphProblems(g, WorkflowTypeMessages, nil); len(problems) != 0 {
		t.Fatalf("problems = %v, want the reply to end the path", problems)
	}
}

func TestAReplyToTheAgentMayStillContinueAfterAnswering(t *testing.T) {
	g := agentCallGraph(
		[]Node{
			{ID: "r", Type: NodeTypeActionReplyToAgent, Config: map[string]interface{}{}},
			{ID: "s", Type: NodeTypeActionSendText, Config: map[string]interface{}{"text": "ok"}},
			{ID: "e", Type: NodeTypeEnd},
		},
		[]Edge{{Source: "t", Target: "r"}, {Source: "r", Target: "s"}, {Source: "s", Target: "e"}},
	)
	if problems := GraphProblems(g, WorkflowTypeMessages, nil); len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
}

func TestAPathEndingOnAnotherActionStillNeedsAnEnd(t *testing.T) {
	g := agentCallGraph(
		[]Node{{ID: "s", Type: NodeTypeActionSendText, Config: map[string]interface{}{"text": "ok"}}},
		[]Edge{{Source: "t", Target: "s"}},
	)
	problems := GraphProblems(g, WorkflowTypeMessages, nil)
	var noEnd, noOutgoing bool
	for _, p := range problems {
		noEnd = noEnd || errors.Is(p, ErrGraphNoEndNode)
		noOutgoing = noOutgoing || errors.Is(p, ErrGraphNodeNoOutgoing)
	}
	if !noEnd || !noOutgoing {
		t.Fatalf("problems = %v, want no end and no outgoing", problems)
	}
}

func TestALoopThatLeavesThroughAReplyToTheAgentIsNotACycleError(t *testing.T) {
	g := agentCallGraph(
		[]Node{
			{ID: "w", Type: NodeTypeWaitForReply, Config: map[string]interface{}{}},
			{ID: "c", Type: NodeTypeConditionBranch, Config: map[string]interface{}{}},
			{ID: "r", Type: NodeTypeActionReplyToAgent, Config: map[string]interface{}{}},
		},
		[]Edge{{Source: "t", Target: "w"}, {Source: "w", Target: "c"}, {Source: "c", Target: "w", Label: "false"}, {Source: "c", Target: "r", Label: "true"}},
	)
	for _, p := range GraphProblems(g, WorkflowTypeMessages, nil) {
		if errors.Is(p, ErrGraphCycleDetected) {
			t.Fatalf("problems = %v, want the reply to count as the loop exit", p)
		}
	}
}

func TestOnlyTheEndAndTheReplyToTheAgentEndAPath(t *testing.T) {
	for _, nt := range []NodeType{NodeTypeEnd, NodeTypeActionReplyToAgent} {
		if !nt.EndsPath() {
			t.Errorf("%s should end a path", nt)
		}
	}
	for _, nt := range []NodeType{NodeTypeActionSendText, NodeTypeTriggerAgentCall, NodeTypeWaitForReply} {
		if nt.EndsPath() {
			t.Errorf("%s should not end a path", nt)
		}
	}
}
