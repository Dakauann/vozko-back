package whatsapp

import (
	"context"
	"encoding/json"
	"testing"

	"vozko/domain/conversation"
)

func TestSendTemplateMessageCarriesQuickReplyPayloadsNextToURLAndCopyCodeButtons(t *testing.T) {
	client, body := captureRequest(t, `{"messages":[{"id":"wamid.1"}]}`)

	_, err := client.SendTemplateMessage(context.Background(), conversation.SendTemplateMessageInput{
		To:           "5511987654321",
		TemplateName: "promo_outubro",
		Language:     "pt_BR",
		Buttons: []conversation.TemplateButtonParam{
			{SubType: conversation.TemplateButtonSubTypeQuickReply, Index: 0, Payload: "vz1:0"},
			{SubType: conversation.TemplateButtonSubTypeQuickReply, Index: 1, Payload: "vz1:1"},
			{SubType: conversation.TemplateButtonSubTypeURL, Index: 2, Text: "pedido-42"},
			{SubType: conversation.TemplateButtonSubTypeCopyCode, Index: 3, CouponCode: "OUTUBRO10"},
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
		`{"type":"button","sub_type":"quick_reply","index":"1","parameters":[{"type":"payload","payload":"vz1:1"}]}`,
		`{"type":"button","sub_type":"url","index":"2","parameters":[{"type":"text","text":"pedido-42"}]}`,
		`{"type":"button","sub_type":"copy_code","index":"3","parameters":[{"type":"coupon_code","coupon_code":"OUTUBRO10"}]}`,
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
