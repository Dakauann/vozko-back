package workflow_usecase

import (
	"errors"
	"fmt"

	"vozko/domain/workflow"
)

const maxNodeRevisits = 3

var errLoopDetected = errors.New("loop detected")

type stepGuard struct {
	state      *workflow.RunState
	durable    int
	executions int
	visits     map[string]int
	insideLoop map[string]bool
	agentCycle map[string]bool
}

func newStepGuard(g *workflow.Graph, state *workflow.RunState) *stepGuard {
	return &stepGuard{
		state:      state,
		durable:    state.GetInt(workflow.StateKeyDurableSteps),
		visits:     map[string]int{},
		insideLoop: loopBodyNodes(g),
		agentCycle: agentCycleNodes(g),
	}
}

func (s *stepGuard) full() error {
	if s.executions >= workflow.MaxExecutionsPerRun {
		return workflow.ErrMaxExecutionsReached
	}
	return nil
}

func (s *stepGuard) admit(node *workflow.Node) error {
	s.durable++
	s.state.Set(workflow.StateKeyDurableSteps, s.durable)
	if s.durable > workflow.MaxDurableExecutionsPerRun {
		return workflow.ErrDurableExecutionsReached
	}
	s.visits[node.ID]++
	if s.visits[node.ID] > s.revisitLimit(node) {
		return fmt.Errorf("%w: node %s visited %d times without waiting", errLoopDetected, node.ID, s.visits[node.ID])
	}
	return nil
}

func (s *stepGuard) revisitLimit(node *workflow.Node) int {
	switch {
	case node.Type == workflow.NodeTypeActionLoop || s.insideLoop[node.ID]:
		return workflow.MaxExecutionsPerRun
	case s.agentCycle[node.ID]:
		return maxAgentCycleRevisits
	}
	return maxNodeRevisits
}

func (s *stepGuard) executed() {
	s.executions++
}

func (s *stepGuard) waited() {
	clear(s.visits)
}

func (s *stepGuard) firstStep() bool {
	return s.durable == 0
}
