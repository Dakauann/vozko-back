package workflow_usecase

import (
	"testing"

	"vozko/domain/workflow"
)

func parkedForReply() (*workflow.WorkflowRun, *workflow.Workflow) {
	w := &workflow.Workflow{ID: "w1", Graph: workflow.Graph{
		Nodes: []workflow.Node{{ID: "wait", Type: workflow.NodeTypeWaitForReply}, {ID: "next", Type: workflow.NodeTypeEnd}},
		Edges: []workflow.Edge{{Source: "wait", Target: "next", Label: "replied"}},
	}}
	state := workflow.NewRunState()
	state.Set(workflow.DataKeyTriggerMessageID, "msg-that-started-the-run")
	return &workflow.WorkflowRun{ID: "r1", WorkflowID: "w1", CurrentNodeID: "wait", State: state}, w
}

func TestAReplyReplacesTheTriggeringMessage(t *testing.T) {
	run, w := parkedForReply()
	data := map[string]interface{}{"message": "sim"}
	workflow.ApplyTriggerMessage(data, "msg-reply")
	if err := AdvanceOnReply(run, w, data); err != nil {
		t.Fatal(err)
	}
	if got := workflow.TriggerMessageID(&run.State); got != "msg-reply" {
		t.Fatalf("trigger message = %q, want the reply", got)
	}
}

func TestAReplyWithoutItsIDNeverLeavesAnOlderMessageAsEvidence(t *testing.T) {
	run, w := parkedForReply()
	if err := AdvanceOnReply(run, w, map[string]interface{}{"message": "sim"}); err != nil {
		t.Fatal(err)
	}
	if got := workflow.TriggerMessageID(&run.State); got != "" {
		t.Fatalf("trigger message = %q; a consent must never cite the message that started the run as the reply", got)
	}
}
