package tools_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/actor"
	"vozko/domain/label"
	"vozko/domain/tools"
)

type workspaceLabels struct {
	labels []*label.Label
	asked  string
}

func (w *workspaceLabels) Execute(workspaceID string) ([]*label.Label, error) {
	w.asked = workspaceID
	return w.labels, nil
}

type labelerSpy struct {
	workspaceID string
	got         []label.LabelChangeRequest
	change      label.LabelChange
	err         error
}

func (s *labelerSpy) Change(workspaceID string, request label.LabelChangeRequest) (label.LabelChange, error) {
	s.workspaceID = workspaceID
	s.got = append(s.got, request)
	return s.change, s.err
}

func labelFixture() *workspaceLabels {
	return &workspaceLabels{labels: []*label.Label{{ID: "vip", Name: "VIP"}, {ID: "urgent", Name: "Urgente"}}}
}

func labelToolFor(labels *workspaceLabels, labeler *labelerSpy) *manageEntryLabelTool {
	return NewManageEntryLabelTool(labels, labeler).(*manageEntryLabelTool)
}

func labelConversationConfig(entryType string) map[string]interface{} {
	return map[string]interface{}{
		"__entry_id":     "entry-1",
		"__entry_type":   entryType,
		"__workspace_id": "ws-1",
		"__agent_id":     "agent-1",
	}
}

func TestTheLabelToolOffersTheWorkspacesLabels(t *testing.T) {
	labels := labelFixture()
	def := labelToolFor(labels, &labelerSpy{}).DefinitionWithContext(tools.ToolContext{WorkspaceID: "ws-1"})

	enum := def.Parameters["label_name"].Enum
	if labels.asked != "ws-1" || len(enum) != 2 || enum[0] != "VIP" || enum[1] != "Urgente" {
		t.Fatalf("enum = %v asked = %q", enum, labels.asked)
	}
	if def.Name != ManageEntryLabelToolName || len(def.Required) != 1 || def.Required[0] != "label_name" {
		t.Fatalf("definition = %+v", def)
	}
	actions := def.Parameters["action"].Enum
	if len(actions) != 2 || actions[0] != "add" || actions[1] != "remove" {
		t.Fatalf("actions = %v", actions)
	}
}

func TestTheAgentLabelsTheConversationOnAnyChannel(t *testing.T) {
	for _, channel := range []string{"whatsapp", "unofficial_whatsapp", "instagram", "facebook", "telegram"} {
		labeler := &labelerSpy{change: label.LabelChange{LabelID: "vip", LabelName: "VIP"}}
		result, err := labelToolFor(labelFixture(), labeler).ExecuteWithConfig(context.Background(), labelConversationConfig(channel), map[string]interface{}{"label_name": " vip "})

		want := label.LabelChangeRequest{Action: label.LabelActionAdd, LabelID: "vip", EntryID: "entry-1", EntryType: channel, ActorID: actor.FormatAI("agent-1")}
		if err != nil || result.IsError || labeler.workspaceID != "ws-1" || len(labeler.got) != 1 || labeler.got[0] != want {
			t.Fatalf("%s: result = %+v, applied = %+v", channel, result, labeler.got)
		}
	}
}

func TestWithoutAnAgentTheLabelIsThePlatformAIs(t *testing.T) {
	labeler := &labelerSpy{change: label.LabelChange{LabelID: "vip", LabelName: "VIP"}}
	config := labelConversationConfig("instagram")
	delete(config, "__agent_id")

	labelToolFor(labelFixture(), labeler).ExecuteWithConfig(context.Background(), config, map[string]interface{}{"label_name": "VIP"})

	if labeler.got[0].ActorID != actor.PlatformAI {
		t.Fatalf("actor = %q", labeler.got[0].ActorID)
	}
}

func TestAnUnknownLabelIsRefusedWithTheAvailableNames(t *testing.T) {
	labeler := &labelerSpy{}
	result, _ := labelToolFor(labelFixture(), labeler).ExecuteWithConfig(context.Background(), labelConversationConfig("telegram"), map[string]interface{}{"label_name": "Clientes antigos"})

	if !result.IsError || !strings.Contains(fmt.Sprint(result.Result), "VIP") || len(labeler.got) != 0 {
		t.Fatalf("an invented label must never be applied: %+v", result)
	}
}

func TestTheLabelToolNeedsAConversation(t *testing.T) {
	labeler := &labelerSpy{}
	result, _ := labelToolFor(labelFixture(), labeler).ExecuteWithConfig(context.Background(), map[string]interface{}{}, map[string]interface{}{"label_name": "VIP"})

	if !result.IsError || len(labeler.got) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestALabelTheConversationAlreadyHasIsNotAnError(t *testing.T) {
	labeler := &labelerSpy{change: label.LabelChange{LabelID: "vip", LabelName: "VIP", Unchanged: true}}
	result, _ := labelToolFor(labelFixture(), labeler).ExecuteWithConfig(context.Background(), labelConversationConfig("facebook"), map[string]interface{}{"label_name": "VIP"})

	if result.IsError || !strings.Contains(fmt.Sprint(result.Result), "já") {
		t.Fatalf("result = %+v", result)
	}
}

func TestAFailedLabelIsReportedAsAnError(t *testing.T) {
	labeler := &labelerSpy{err: errors.New("db down")}
	result, _ := labelToolFor(labelFixture(), labeler).ExecuteWithConfig(context.Background(), labelConversationConfig("whatsapp"), map[string]interface{}{"label_name": "VIP"})

	if !result.IsError {
		t.Fatalf("result = %+v", result)
	}
}

func TestTheAgentRemovesALabel(t *testing.T) {
	labeler := &labelerSpy{change: label.LabelChange{LabelID: "vip", LabelName: "VIP"}}
	result, _ := labelToolFor(labelFixture(), labeler).ExecuteWithConfig(context.Background(), labelConversationConfig("instagram"), map[string]interface{}{"action": "remove", "label_name": "VIP"})

	if result.IsError || len(labeler.got) != 1 || labeler.got[0].Action != label.LabelActionRemove || !strings.Contains(fmt.Sprint(result.Result), "removida") {
		t.Fatalf("result = %+v, requests = %+v", result, labeler.got)
	}
}

func TestAnUnknownLabelActionIsRefusedBeforeAnythingChanges(t *testing.T) {
	labeler := &labelerSpy{}
	result, _ := labelToolFor(labelFixture(), labeler).ExecuteWithConfig(context.Background(), labelConversationConfig("instagram"), map[string]interface{}{"action": "toggle", "label_name": "VIP"})

	if !result.IsError || len(labeler.got) != 0 {
		t.Fatalf("result = %+v", result)
	}
}
