package workflow

import (
	"errors"
	"reflect"
	"testing"
)

func triggerNodes(types ...NodeType) []Node {
	nodes := []Node{{ID: "end", Type: NodeTypeEnd}}
	for _, t := range types {
		nodes = append(nodes, Node{ID: string(t), Type: t})
	}
	return nodes
}

func TestAButtonStartBeginsAtTheMessageTriggerTheCustomerWouldHaveFired(t *testing.T) {
	cases := []struct {
		name  string
		nodes []Node
		want  NodeType
	}{
		{"message received only", triggerNodes(NodeTypeTriggerMessageReceived), NodeTypeTriggerMessageReceived},
		{"first message only", triggerNodes(NodeTypeTriggerFirstMessage), NodeTypeTriggerFirstMessage},
		{"message received wins over first message", triggerNodes(NodeTypeTriggerFirstMessage, NodeTypeTriggerMessageReceived), NodeTypeTriggerMessageReceived},
		{"a webhook beside the message trigger is never chosen", triggerNodes(NodeTypeTriggerWebhook, NodeTypeTriggerFirstMessage), NodeTypeTriggerFirstMessage},
		{"an agent call primary is never chosen", triggerNodes(NodeTypeTriggerAgentCall, NodeTypeTriggerMessageReceived), NodeTypeTriggerMessageReceived},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MessageStartTrigger(tc.nodes)
			if err != nil {
				t.Fatal(err)
			}
			if got.Type != tc.want || got != &tc.nodes[indexOfType(tc.nodes, tc.want)] {
				t.Fatalf("start = %+v, want the %s node of the graph", got, tc.want)
			}
		})
	}
}

func indexOfType(nodes []Node, t NodeType) int {
	for i := range nodes {
		if nodes[i].Type == t {
			return i
		}
	}
	return -1
}

func TestAWorkflowWithoutAMessageTriggerCannotStartFromAButton(t *testing.T) {
	cases := []struct {
		name  string
		nodes []Node
	}{
		{"no trigger", triggerNodes()},
		{"only a webhook", triggerNodes(NodeTypeTriggerWebhook)},
		{"agent call and form", triggerNodes(NodeTypeTriggerAgentCall, NodeTypeTriggerFormSubmitted)},
		{"stage and birthday", triggerNodes(NodeTypeTriggerStageAdded, NodeTypeTriggerBirthday)},
		{"nothing at all", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MessageStartTrigger(tc.nodes)
			if got != nil || !errors.Is(err, ErrWorkflowNeedsMessageTrigger) {
				t.Fatalf("start = %+v err = %v, want ErrWorkflowNeedsMessageTrigger", got, err)
			}
		})
	}
}

func TestTheMessageStartTypesAreTheOnesTheRuleAccepts(t *testing.T) {
	types := MessageStartTriggerTypes()
	if !reflect.DeepEqual(types, []TriggerType{TriggerMessageReceived, TriggerFirstMessage}) {
		t.Fatalf("types = %v", types)
	}
	types[0] = TriggerWebhook
	if MessageStartTriggerTypes()[0] != TriggerMessageReceived {
		t.Fatal("a caller must not change the rule through the returned list")
	}
	for _, typ := range MessageStartTriggerTypes() {
		if _, err := MessageStartTrigger(triggerNodes(NodeType(typ))); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
}

func TestAButtonStartCarriesWhatALiveTapCarries(t *testing.T) {
	tap := TemplateButtonSelection("Quero o catálogo", 1, true)
	got := MessageStartRunData(MessageStart{
		Text: "Quero o catálogo", Channel: "whatsapp", WorkspaceID: "ws1", Selection: tap, ContactNumber: " 5511999990000 ", TriggerMessageID: "row-42",
	})
	want := InboundMessageData("Quero o catálogo", InboundTextMessage, "", tap)
	want[VarChannel] = "whatsapp"
	want[DataKeyWorkspaceID] = "ws1"
	ApplyContactNumber(want, "5511999990000")
	ApplyTriggerMessage(want, "row-42")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("data = %v, want %v", got, want)
	}
	if got[DataKeySelectedOptionID] != TemplateButtonOptionID(1) || got[DataKeySelectedOptionKind] != OptionKindTemplateButton || got[DataKeyMessageType] != "text" {
		t.Fatalf("data = %v", got)
	}
}

func TestAnOperatorStartIsAnEmptyTypedMessageUnlessOneIsGiven(t *testing.T) {
	got := MessageStartRunData(MessageStart{Channel: "instagram", WorkspaceID: "ws1"})
	want := map[string]interface{}{DataKeyMessage: "", DataKeyMessageType: "text", VarChannel: "instagram", DataKeyWorkspaceID: "ws1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("data = %v, want %v", got, want)
	}
}
