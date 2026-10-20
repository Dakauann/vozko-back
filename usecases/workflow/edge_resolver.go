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
	ErrRunNotAtFormNode          = errors.New("run is not parked at a form node")
	ErrMissingCompletedEdge      = errors.New("action_send_form is missing the required 'completed' output edge")
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

	advanceParkedRun(run, nextID, outcome, data)
	return nil
}

func AdvanceOnForm(run *workflow.WorkflowRun, w *workflow.Workflow, data map[string]interface{}) error {
	node := w.Graph.FindNode(run.CurrentNodeID)
	if node == nil || !node.Type.IsFormPrompt() {
		return ErrRunNotAtFormNode
	}
	nextID := findExactEdgeByLabel(w.Graph.OutgoingEdges(run.CurrentNodeID), workflow.FormHandleCompleted)
	if nextID == "" {
		return ErrMissingCompletedEdge
	}
	advanceParkedRun(run, nextID, workflow.FormHandleCompleted, data)
	return nil
}

func advanceParkedRun(run *workflow.WorkflowRun, nextID, outcome string, data map[string]interface{}) {
	run.State.Set("_wait_outcome", outcome)
	workflow.ForgetTriggerMessage(&run.State)
	for k, v := range data {
		run.State.Set(k, v)
	}
	run.State.Set("_prev_node_id", run.CurrentNodeID)
	run.CurrentNodeID = nextID
	run.SetRunning()
	run.UpdatedAt = time.Now().UTC()
}

func unansweredOutcome(node workflow.NodeType) string {
	if node == workflow.NodeTypeWaitForReply {
		return "timeout"
	}
	return workflow.InteractiveHandleNoReply
}

func unansweredEdge(run *workflow.WorkflowRun, w *workflow.Workflow, node *workflow.Node) string {
	outcome := unansweredOutcome(node.Type)
	run.State.Set("_wait_outcome", outcome)
	return findExactEdgeByLabel(w.Graph.OutgoingEdges(node.ID), outcome)
}

func expiresUnanswered(node workflow.NodeType, reason workflow.WaitReason) bool {
	if node.IsFormPrompt() {
		return reason == workflow.WaitReasonForm
	}
	return node == workflow.NodeTypeWaitForReply || node.IsInteractivePrompt()
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

func timerElapsedEdge(run *workflow.WorkflowRun, w *workflow.Workflow, node *workflow.Node) (string, error) {
	run.State.Set("_wait_outcome", "completed")
	nextID := resolveRequiredWaitEdge(w.Graph.OutgoingEdges(node.ID), "completed")
	if nextID == "" {
		return "", &workflow.MissingOutputError{NodeID: node.ID, OutputID: "completed"}
	}
	return nextID, nil
}

func resolveRequiredWaitEdge(edges []workflow.Edge, label string) string {
	return findExactEdgeByLabel(edges, label)
}
