package node_executors

import (
	"testing"

	"vozko/domain/actor"
	"vozko/domain/label"
	"vozko/domain/workflow"
)

type labelerStub struct {
	workspaceID string
	got         []label.LabelChangeRequest
	change      label.LabelChange
	err         error
}

func (s *labelerStub) Change(workspaceID string, request label.LabelChangeRequest) (label.LabelChange, error) {
	s.workspaceID = workspaceID
	s.got = append(s.got, request)
	return s.change, s.err
}

func labelNodeCtx(config map[string]interface{}) *workflow.NodeContext {
	state := workflow.NewRunState()
	return &workflow.NodeContext{
		Node:  &workflow.Node{ID: "n1", Config: config},
		Graph: &workflow.Graph{Edges: []workflow.Edge{{Source: "n1", Target: "ok", Label: "sucesso"}, {Source: "n1", Target: "fail", Label: "erro"}}},
		Run:   &workflow.WorkflowRun{ID: "run1", WorkflowID: "wf-1", WorkspaceID: "ws1", EntryID: "entry1", EntryType: "telegram"},
		State: &state,
	}
}

func TestAnExistingLabelNodeStillAddsTheLabelAsTheWorkflow(t *testing.T) {
	labeler := &labelerStub{change: label.LabelChange{LabelID: "vip", LabelName: "VIP"}}

	result, err := NewAssignLabelExecutor(labeler).Execute(labelNodeCtx(map[string]interface{}{"label_id": "vip"}))

	if err != nil || result.NextNodeID != "ok" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	want := label.LabelChangeRequest{Action: label.LabelActionAdd, LabelID: "vip", EntryID: "entry1", EntryType: "telegram", ActorID: actor.FormatWorkflow("wf-1")}
	if labeler.workspaceID != "ws1" || len(labeler.got) != 1 || labeler.got[0] != want {
		t.Fatalf("changed %+v in %s", labeler.got, labeler.workspaceID)
	}
	if result.Output["label_name"] != "VIP" || result.Output["success"] != true || result.Output["changed"] != true {
		t.Fatalf("output = %v", result.Output)
	}
}

func TestTheLabelNodeRemovesALabel(t *testing.T) {
	labeler := &labelerStub{change: label.LabelChange{LabelID: "vip", LabelName: "VIP"}}

	result, _ := NewAssignLabelExecutor(labeler).Execute(labelNodeCtx(map[string]interface{}{"label_id": "vip", "action": "remove"}))

	if result.NextNodeID != "ok" || labeler.got[0].Action != label.LabelActionRemove {
		t.Fatalf("result = %+v, request = %+v", result, labeler.got)
	}
}

func TestANoOpLabelChangeFollowsTheSuccessPath(t *testing.T) {
	labeler := &labelerStub{change: label.LabelChange{LabelID: "vip", LabelName: "VIP", Unchanged: true}}

	result, _ := NewAssignLabelExecutor(labeler).Execute(labelNodeCtx(map[string]interface{}{"label_id": "vip"}))

	if result.NextNodeID != "ok" || result.Output["success"] != true || result.Output["changed"] != false {
		t.Fatalf("result = %+v", result)
	}
}

func TestARefusedLabelFollowsTheErrorPath(t *testing.T) {
	labeler := &labelerStub{err: label.ErrUnauthorized}

	result, err := NewAssignLabelExecutor(labeler).Execute(labelNodeCtx(map[string]interface{}{"label_id": "vip"}))

	if err != nil || result.NextNodeID != "fail" || result.Output["success"] != false {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestAnUnknownLabelActionFollowsTheErrorPathWithoutChangingAnything(t *testing.T) {
	labeler := &labelerStub{}

	result, _ := NewAssignLabelExecutor(labeler).Execute(labelNodeCtx(map[string]interface{}{"label_id": "vip", "action": "toggle"}))

	if result.NextNodeID != "fail" || len(labeler.got) != 0 {
		t.Fatalf("result = %+v, requests = %v", result, labeler.got)
	}
}

func TestTheLabelNodeNeedsALabel(t *testing.T) {
	if _, err := NewAssignLabelExecutor(&labelerStub{}).Execute(labelNodeCtx(map[string]interface{}{"label_id": "  "})); err != workflow.ErrNodeConfigMissing {
		t.Fatalf("err = %v", err)
	}
}
