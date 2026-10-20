package workflow

import (
	"errors"
	"strings"
	"time"

	"vozko/domain/redact"
	"vozko/domain/tracepayload"
)

var ErrNoTriggerData = errors.New("workflow: this run kept no trigger data (it started before trigger data was stored and its trigger step has no output), so it cannot be reproduced")

func TriggerDataOf(state RunState) map[string]interface{} {
	if len(state.Vars) == 0 {
		return nil
	}
	start := make(map[string]interface{}, len(state.Vars))
	for k, v := range state.Vars {
		if strings.HasPrefix(k, "_") {
			continue
		}
		start[k] = v
	}
	if len(start) == 0 {
		return nil
	}
	return tracepayload.Map(redact.New().Map(start))
}

func ReproductionSeed(run *WorkflowRun, steps []*WorkflowRunLog) (map[string]interface{}, error) {
	if run != nil && len(run.TriggerData) > 0 {
		return cloneConfigMap(run.TriggerData), nil
	}
	for _, step := range steps {
		if step != nil && step.NodeType.IsTrigger() && len(step.Output) > 0 {
			return cloneConfigMap(step.Output), nil
		}
	}
	return nil, ErrNoTriggerData
}

func replayedFromRun(n Node, behaviors TestBehaviors) bool {
	return n.Type == NodeTypeActionHTTPRequest || n.Type == NodeTypeActionCode || SideEffectNode(n, behaviors)
}

func ReproductionPins(draft *Graph, steps []*WorkflowRunLog, runID string, at time.Time, behaviors TestBehaviors) map[string]NodePin {
	replayed := make(map[string]bool)
	for _, id := range replayedNodeIDs(draft, behaviors) {
		replayed[id] = true
	}
	pins := make(map[string]NodePin)
	for _, step := range steps {
		if step == nil || !replayed[step.NodeID] {
			continue
		}
		if pin, err := PinFromStep(step, runID, at); err == nil {
			pins[step.NodeID] = pin
		}
	}
	return pins
}
