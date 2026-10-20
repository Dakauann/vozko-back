package workflow

import (
	"testing"

	"vozko/domain/conversation"
)

func TestASandboxStepCarriesWhatItWouldHaveDoneWithoutTouchingTheStoredRow(t *testing.T) {
	row := &WorkflowRunLog{ID: "s1", NodeID: "n1", Output: map[string]interface{}{"success": true}}
	change := SimulatedChange{Kind: SimulatedLabel, Action: "add", TargetID: "l1", TargetName: "VIP", Description: "Adicionaria a etiqueta VIP"}
	sent := conversation.Message{ID: "m1", MessageType: conversation.MessageTypeAIResponse, Text: "Olá"}

	out := WithSandboxEffects(row, SandboxEffects{Changes: []SimulatedChange{change}, Messages: []conversation.Message{sent}})

	if !out.Simulated {
		t.Fatal("a step with a would-do change is simulated")
	}
	wouldDo, ok := out.Output[StepOutputWouldDo].([]interface{})
	if !ok || len(wouldDo) != 1 {
		t.Fatalf("would_do = %#v", out.Output[StepOutputWouldDo])
	}
	first := wouldDo[0].(map[string]interface{})
	if first["kind"] != "label" || first["target_name"] != "VIP" || first["description"] != "Adicionaria a etiqueta VIP" {
		t.Fatalf("change = %v", first)
	}
	messages, ok := out.Output[StepOutputSentMessages].([]interface{})
	if !ok || len(messages) != 1 || messages[0].(map[string]interface{})["text"] != "Olá" || messages[0].(map[string]interface{})["messageType"] != "ai_response" {
		t.Fatalf("sent_messages = %#v, want the CRM message wire", out.Output[StepOutputSentMessages])
	}
	if out.Output["success"] != true {
		t.Fatal("the executor output stays")
	}
	if _, touched := row.Output[StepOutputWouldDo]; touched || row.Simulated {
		t.Fatal("the stored row must not change")
	}
}

func TestASandboxStepWithOnlyMessagesIsNotASimulatedWrite(t *testing.T) {
	row := &WorkflowRunLog{ID: "s1"}
	out := WithSandboxEffects(row, SandboxEffects{Messages: []conversation.Message{{ID: "m1", Text: "oi"}}})
	if out.Simulated {
		t.Fatal("sending a message is not a CRM write")
	}
	if _, ok := out.Output[StepOutputSentMessages]; !ok {
		t.Fatal("the message is missing from a step that had no output")
	}
}

func TestAStepWithoutSandboxEffectsIsReturnedAsIs(t *testing.T) {
	row := &WorkflowRunLog{ID: "s1", Output: map[string]interface{}{"a": 1}}
	if out := WithSandboxEffects(row, SandboxEffects{}); out != row {
		t.Fatal("no effects, no copy")
	}
	if WithSandboxEffects(nil, SandboxEffects{Changes: []SimulatedChange{{Kind: SimulatedLabel}}}) != nil {
		t.Fatal("a missing row stays missing")
	}
}
