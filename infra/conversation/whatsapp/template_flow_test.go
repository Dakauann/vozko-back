package whatsapp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/conversation"
)

func TestSendTemplateMessageCarriesTheFormToken(t *testing.T) {
	client, body := captureRequest(t, `{"messages":[{"id":"wamid.1"}]}`)

	_, err := client.SendTemplateMessage(context.Background(), conversation.SendTemplateMessageInput{
		To:           "5511987654321",
		TemplateName: "cadastro",
		Language:     "pt_BR",
		Buttons: []conversation.TemplateButtonParam{
			{SubType: conversation.TemplateButtonSubTypeQuickReply, Index: 0, Payload: "vz1:0"},
			{SubType: conversation.TemplateButtonSubTypeFlow, Index: 1, FlowToken: "tok_1"},
		},
	})
	if err != nil {
		t.Fatalf("SendTemplateMessage: %v", err)
	}

	var payload struct {
		Template struct {
			Components []json.RawMessage `json:"components"`
		} `json:"template"`
	}
	if err := json.Unmarshal(*body, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []string{
		`{"type":"button","sub_type":"quick_reply","index":"0","parameters":[{"type":"payload","payload":"vz1:0"}]}`,
		`{"type":"button","sub_type":"flow","index":"1","parameters":[{"type":"action","action":{"flow_token":"tok_1"}}]}`,
	}
	if len(payload.Template.Components) != len(want) {
		t.Fatalf("components = %s", *body)
	}
	for i, component := range payload.Template.Components {
		if string(component) != want[i] {
			t.Errorf("component %d = %s, want %s", i, component, want[i])
		}
	}
}

func TestCreatingATemplateSendsTheFlowButtonFields(t *testing.T) {
	cases := []struct {
		name   string
		button conversation.TemplateButton
		want   string
	}{
		{
			name:   "navigate",
			button: conversation.TemplateButton{Type: "flow", Text: "Responder", FlowID: "F1", FlowAction: " NAVIGATE ", NavigateScreen: "S_INICIO"},
			want:   `{"type":"FLOW","text":"Responder","flow_id":"F1","flow_action":"navigate","navigate_screen":"S_INICIO"}`,
		},
		{
			name:   "default action",
			button: conversation.TemplateButton{Type: "FLOW", Text: "Responder", FlowID: "F1", NavigateScreen: "S_INICIO"},
			want:   `{"type":"FLOW","text":"Responder","flow_id":"F1","navigate_screen":"S_INICIO"}`,
		},
		{
			name:   "other buttons carry no flow fields",
			button: conversation.TemplateButton{Type: "QUICK_REPLY", Text: "Sim"},
			want:   `{"type":"QUICK_REPLY","text":"Sim"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := toWireComponents([]conversation.TemplateComponent{{Type: "BUTTONS", Buttons: []conversation.TemplateButton{tc.button}}})

			got, err := json.Marshal(wire[0].Buttons[0])

			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("button = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestASyncedFlowButtonKeepsItsFlow(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{name: "numeric flow id", raw: `{"type":"FLOW","text":"Responder","flow_id":1234567890,"flow_action":"NAVIGATE","navigate_screen":"S_INICIO"}`},
		{name: "string flow id", raw: `{"type":"FLOW","text":"Responder","flow_id":"1234567890","flow_action":"navigate","navigate_screen":"S_INICIO"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var response templateResponse
			if err := json.Unmarshal([]byte(`{"id":"T1","name":"cadastro","components":[{"type":"BODY","text":"Oi"},{"type":"BUTTONS","buttons":[`+tc.raw+`]}]}`), &response); err != nil {
				t.Fatal(err)
			}

			synced := mapTemplateResponse(response)

			want := conversation.TemplateButton{Type: "FLOW", Text: "Responder", FlowID: "1234567890", FlowAction: "navigate", NavigateScreen: "S_INICIO"}
			if got := synced.Components[1].Buttons[0]; !reflect.DeepEqual(got, want) {
				t.Fatalf("button = %+v", got)
			}
		})
	}
}

func TestATemplateWithAFormButtonCanBeEdited(t *testing.T) {
	server, client := newMetaEditServer(t, `{"id":"x1","status":"APPROVED","components":[
		{"type":"BODY","text":"Oi"},
		{"type":"BUTTONS","buttons":[{"type":"QUICK_REPLY","text":"Sim"},{"type":"FLOW","text":"Abrir","flow_id":123,"flow_action":"NAVIGATE","navigate_screen":"S_INICIO"}]}
	]}`)

	err := client.EditTemplate(context.Background(), "x1", conversation.EditTemplateInput{Components: []conversation.TemplateComponent{
		{Type: "BODY", Text: "Oi, responda o cadastro."},
		{Type: "BUTTONS", Buttons: []conversation.TemplateButton{
			{Type: "QUICK_REPLY", Text: "Sim"},
			{Type: "FLOW", Text: "Responder", FlowID: "123", FlowAction: "navigate", NavigateScreen: "S_INICIO"},
		}},
	}})

	if err != nil {
		t.Fatalf("EditTemplate: %v", err)
	}
	if len(server.edits) != 1 {
		t.Fatalf("edits = %d", len(server.edits))
	}
	_, components := decodeEdit(t, server.edits[0])
	buttons, _ := json.Marshal(components[1])
	if !strings.Contains(string(buttons), `{"flow_action":"navigate","flow_id":"123","navigate_screen":"S_INICIO","text":"Responder","type":"FLOW"}`) {
		t.Fatalf("buttons = %s", buttons)
	}
}
