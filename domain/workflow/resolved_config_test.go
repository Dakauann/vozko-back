package workflow

import (
	"reflect"
	"testing"
)

func TestTheStepParamsOfASendShowWhatTheCustomerReceived(t *testing.T) {
	state := customerState(map[string]interface{}{"lead_name": "Ana", "pago": true})
	node := &Node{ID: "n1", Type: NodeTypeActionSendInteractive, Config: map[string]interface{}{
		"body":    "Oi {{lead_name}}, pago: {{pago}} {{input.telefone}}",
		"buttons": []interface{}{map[string]interface{}{"id": "a", "title": "Sou {{lead_name}}"}},
		"timeout": 30.0,
	}}

	got := ResolvedConfig(node, state)

	want := map[string]interface{}{
		"body":    "Oi Ana, pago: Sim ",
		"buttons": []interface{}{map[string]interface{}{"id": "a", "title": "Sou Ana"}},
		"timeout": 30.0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolvedConfig = %#v, want %#v", got, want)
	}
}

func TestTheStepParamsOfARequestKeepRawValuesAndTheUnresolvedTemplate(t *testing.T) {
	state := customerState(map[string]interface{}{"pago": true})
	node := &Node{ID: "n2", Type: NodeTypeActionHTTPRequest, Config: map[string]interface{}{
		"body":    `{"pago": {{pago}}, "cpf": "{{input.cpf}}"}`,
		"headers": []interface{}{"X-Pago: {{pago}}"},
	}}

	got := ResolvedConfig(node, state)

	if got["body"] != `{"pago": true, "cpf": "{{input.cpf}}"}` {
		t.Fatalf("body = %v", got["body"])
	}
	if !reflect.DeepEqual(got["headers"], []interface{}{"X-Pago: true"}) {
		t.Fatalf("headers = %v, want templates inside lists resolved too", got["headers"])
	}
}

func TestTheStepParamsOfCodeAreTheScriptAsWritten(t *testing.T) {
	node := &Node{ID: "n3", Type: NodeTypeActionCode, Config: map[string]interface{}{"code": "return {a: '{{x}}'}"}}
	if got := ResolvedConfig(node, customerState(map[string]interface{}{"x": "1"})); got["code"] != "return {a: '{{x}}'}" {
		t.Fatalf("code = %v", got["code"])
	}
}

func TestOnlyMessagingNodesSendToTheCustomer(t *testing.T) {
	for _, nt := range []NodeType{NodeTypeActionSendText, NodeTypeActionSendTemplate, NodeTypeActionSendMedia, NodeTypeActionSendInteractive, NodeTypeActionSendForm, NodeTypeActionSendCarrier, NodeTypeActionSendPlatformEmail} {
		if !nt.SendsToCustomer() {
			t.Errorf("%s sends to the customer", nt)
		}
	}
	for _, nt := range []NodeType{NodeTypeActionSendEmail, NodeTypeActionHTTPRequest, NodeTypeActionSetVariable, NodeTypeConditionBranch, NodeTypeActionReplyToAgent, NodeTypeActionAIAgent} {
		if nt.SendsToCustomer() {
			t.Errorf("%s does not send customer text from its config", nt)
		}
	}
}
