package workflow_usecase

import (
	"errors"
	"time"

	"vozko/domain/workflow"
)

var (
	ErrRunNotAtWaitNode          = errors.New("run is not parked at a wait node")
	ErrMissingRepliedEdge        = errors.New("wait_for_reply is missing the required 'replied' output edge")
	ErrInteractiveReplyUnhandled = errors.New("interactive prompt reply matched no wired output")
)

func AdvanceOnReply(run *workflow.WorkflowRun, w *workflow.Workflow, data map[string]interface{}) error {
	node := w.Graph.FindNode(run.CurrentNodeID)
	if node == nil || !node.Type.ParksForReply() {
		return ErrRunNotAtWaitNode
	}
	edges := w.Graph.OutgoingEdges(run.CurrentNodeID)

	var nextID, outcome string
	if node.Type.IsInteractivePrompt() {
		optionID, _ := data["selected_option_id"].(string)
		nextID, outcome = resolveInteractiveReplyEdge(edges, optionID)
		if nextID == "" {
			return ErrInteractiveReplyUnhandled
		}
	} else {
		nextID = resolveRequiredWaitEdge(edges, "replied")
		outcome = "replied"
		if nextID == "" {
			return ErrMissingRepliedEdge
		}
	}

	run.State.Set("_wait_outcome", outcome)
	for k, v := range data {
		run.State.Set(k, v)
	}
	run.State.Set("_prev_node_id", run.CurrentNodeID)
	run.CurrentNodeID = nextID
	run.SetRunning()
	run.UpdatedAt = time.Now().UTC()
	return nil
}

func resolveInteractiveReplyEdge(edges []workflow.Edge, optionID string) (string, string) {
	if optionID != "" {
		if target := findExactEdgeByLabel(edges, optionID); target != "" {
			return target, optionID
		}
	}
	if target := findExactEdgeByLabel(edges, "no_match"); target != "" {
		return target, "no_match"
	}
	if target := firstDefaultEdge(edges); target != "" {
		return target, "no_match"
	}
	return "", ""
}

func firstDefaultEdge(edges []workflow.Edge) string {
	for _, e := range edges {
		if e.Label == "" || e.Label == "default" {
			return e.Target
		}
	}
	return ""
}

func resolveEdgeByLabel(edges []workflow.Edge, label string) string {
	if label != "" {
		for _, e := range edges {
			if e.Label == label {
				return e.Target
			}
		}
	}

	if len(edges) > 0 {
		return edges[0].Target
	}
	return ""
}

func findExactEdgeByLabel(edges []workflow.Edge, label string) string {
	if label == "" {
		return ""
	}
	for _, e := range edges {
		if e.Label == label {
			return e.Target
		}
	}
	return ""
}

func resolveRequiredWaitEdge(edges []workflow.Edge, label string) string {
	return findExactEdgeByLabel(edges, label)
}
