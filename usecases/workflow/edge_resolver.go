package workflow_usecase

import (
	"errors"
	"time"

	"vozko/domain/workflow"
)

// Errors returned by AdvanceOnReply so callers can surface them appropriately.
var (
	ErrRunNotAtWaitNode   = errors.New("run is not parked at a wait node")
	ErrMissingRepliedEdge = errors.New("wait_for_reply is missing the required 'replied' output edge")
	// ErrInteractiveReplyUnhandled means the contact replied to an interactive
	// prompt but the reply matched no option edge, no 'no_match' edge, and no
	// legacy default edge. Callers treat it as "leave the run PARKED" (ignore the
	// stray reply) so a later valid selection — or the timeout ('no_reply') — can
	// still resume it, rather than erroring the run over unexpected input.
	ErrInteractiveReplyUnhandled = errors.New("interactive prompt reply matched no wired output")
)

// AdvanceOnReply moves a run parked at a reply-waiting node onto the correct
// outgoing edge, recording the reply data into state. It is the single source of
// truth for reply-resume semantics — shared by production message routing
// (trigger_evaluator.wakeRunForReply) and the workflow simulator — so both
// resume identically. It handles two parked node kinds:
//   - wait_for_reply: always routes to the required "replied" edge.
//   - interactive prompt (send buttons/list): routes to the edge whose label is
//     the selected option id, else "no_match", else a legacy default edge.
//
// It does NOT persist or execute; the caller updates the repo and runs the engine.
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

// resolveInteractiveReplyEdge picks the outgoing edge for an interactive prompt
// reply: the tapped option's own id first, then the "no_match" catch-all, then a
// legacy single default edge (the pre-branching send_whatsapp_button wiring).
// Returns ("", "") when nothing matches so the caller can leave the run parked.
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

// firstDefaultEdge returns the target of a single unlabeled / "default" edge, the
// wiring a send_whatsapp_button node carried before per-option branching existed.
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
