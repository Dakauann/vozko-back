package workflow

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
)

func carrierGraph(config map[string]interface{}) *Graph {
	return &Graph{
		Nodes: []Node{
			{ID: "t", Type: NodeTypeTriggerMessageReceived},
			{ID: "send", Type: NodeTypeActionSendCarrier, Config: config},
			{ID: "e", Type: NodeTypeEnd},
		},
		Edges: []Edge{{Source: "t", Target: "send"}, {Source: "send", Target: "e", Label: CarrierHandleSent}},
	}
}

func rcsContent(fields map[string]interface{}) map[string]interface{} {
	return fields
}

func TestSendCarrierIsAMessagingNode(t *testing.T) {
	if NodeTypeActionSendCarrier != "action_send_carrier" || !NodeTypeActionSendCarrier.Valid() || NodeTypeActionSendCarrier.Category() != NodeCategoryMessaging {
		t.Fatalf("node type %q must be a valid messaging node", NodeTypeActionSendCarrier)
	}
}

func TestReadCarrierSend(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		want   CarrierSend
		err    error
	}{
		{
			"rcs with marketing by default",
			map[string]interface{}{CarrierChannelKey: "rcs", CarrierSenderKey: "snd-1", CarrierContentKey: rcsContent(map[string]interface{}{"text": "Oi"})},
			CarrierSend{Channel: carriermessaging.ChannelRCS, SenderID: "snd-1", Content: rcs.Content{Text: "Oi"}, Marketing: true},
			nil,
		},
		{
			"sms uses the route and its text",
			map[string]interface{}{CarrierChannelKey: "sms", CarrierRouteKey: "rt-1", CarrierSenderKey: "snd-1", CarrierTextKey: "Oi", CarrierMarketingKey: false},
			CarrierSend{Channel: carriermessaging.ChannelSMS, SenderID: "rt-1", Text: "Oi", Marketing: false},
			nil,
		},
		{"unknown channel", map[string]interface{}{CarrierChannelKey: "fax"}, CarrierSend{}, ErrCarrierSendUnreadable},
		{"no channel", map[string]interface{}{}, CarrierSend{}, ErrCarrierSendUnreadable},
		{"content that is not an object", map[string]interface{}{CarrierChannelKey: "rcs", CarrierContentKey: "Oi"}, CarrierSend{}, ErrCarrierSendUnreadable},
		{"content in the wrong shape", map[string]interface{}{CarrierChannelKey: "rcs", CarrierContentKey: map[string]interface{}{"cards": "um"}}, CarrierSend{}, ErrCarrierSendUnreadable},
		{"marketing that is not a switch", map[string]interface{}{CarrierChannelKey: "sms", CarrierMarketingKey: "sim"}, CarrierSend{}, ErrCarrierSendUnreadable},
		{"text that is not text", map[string]interface{}{CarrierChannelKey: "sms", CarrierTextKey: 12}, CarrierSend{}, ErrCarrierSendUnreadable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReadCarrierSend(tc.config)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if err != nil {
				return
			}
			if got.Channel != tc.want.Channel || got.SenderID != tc.want.SenderID || got.Text != tc.want.Text || got.Marketing != tc.want.Marketing || got.Content.Text != tc.want.Content.Text {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestCarrierSendRendersEveryTextField(t *testing.T) {
	send := CarrierSend{Channel: carriermessaging.ChannelRCS, Text: "{{a}}", Content: rcs.Content{
		Text:        "Oi {{a}}",
		Suggestions: []rcs.Suggestion{{Kind: rcs.SuggestOpenURL, Label: "{{a}}", URL: "https://x.example/{{a}}"}},
		Cards:       []rcs.Card{{Title: "{{a}}", Description: "d {{a}}", Suggestions: []rcs.Suggestion{{Kind: rcs.SuggestReply, Label: "r {{a}}"}}}},
	}}
	out := send.Render(func(s string) string { return strings.ReplaceAll(s, "{{a}}", "Ana") })
	c := out.Content
	if out.Text != "Ana" || c.Text != "Oi Ana" || c.Suggestions[0].Label != "Ana" || c.Suggestions[0].URL != "https://x.example/Ana" ||
		c.Cards[0].Title != "Ana" || c.Cards[0].Description != "d Ana" || c.Cards[0].Suggestions[0].Label != "r Ana" {
		t.Fatalf("rendered = %+v", out)
	}
	if send.Content.Text != "Oi {{a}}" {
		t.Fatal("render must not change the stored content")
	}
}

func TestValidateCarrierSends(t *testing.T) {
	longSMS := strings.Repeat("a", 160*9)
	cases := []struct {
		name   string
		config map[string]interface{}
		ok     bool
	}{
		{"plain rcs text", map[string]interface{}{CarrierChannelKey: "rcs", CarrierContentKey: rcsContent(map[string]interface{}{"text": "Oi {{lead_name}}"})}, true},
		{"rcs made only of a variable", map[string]interface{}{CarrierChannelKey: "rcs", CarrierContentKey: rcsContent(map[string]interface{}{"text": "{{event.mensagem}}"})}, true},
		{"rcs link from a variable", map[string]interface{}{CarrierChannelKey: "rcs", CarrierContentKey: rcsContent(map[string]interface{}{
			"text": "Seu carrinho", "suggestions": []interface{}{map[string]interface{}{"kind": "open_url", "label": "Abrir", "url": "{{event.cart_url}}"}},
		})}, true},
		{"rcs link with a variable path", map[string]interface{}{CarrierChannelKey: "rcs", CarrierContentKey: rcsContent(map[string]interface{}{
			"text": "Seu carrinho", "suggestions": []interface{}{map[string]interface{}{"kind": "open_url", "label": "Abrir", "url": "https://loja.example/{{event.id}}"}},
		})}, true},
		{"rcs plain http link", map[string]interface{}{CarrierChannelKey: "rcs", CarrierContentKey: rcsContent(map[string]interface{}{
			"text": "Seu carrinho", "suggestions": []interface{}{map[string]interface{}{"kind": "open_url", "label": "Abrir", "url": "http://loja.example"}},
		})}, false},
		{"empty rcs message", map[string]interface{}{CarrierChannelKey: "rcs", CarrierContentKey: rcsContent(map[string]interface{}{})}, false},
		{"rcs without content", map[string]interface{}{CarrierChannelKey: "rcs"}, false},
		{"rcs with cards and text", map[string]interface{}{CarrierChannelKey: "rcs", CarrierContentKey: rcsContent(map[string]interface{}{
			"text": "Oi", "cards": []interface{}{map[string]interface{}{"title": "A"}},
		})}, false},
		{"unreadable config", map[string]interface{}{CarrierChannelKey: "fax"}, false},
		{"sms text", map[string]interface{}{CarrierChannelKey: "sms", CarrierTextKey: "Oi {{lead_name}}"}, true},
		{"sms too long", map[string]interface{}{CarrierChannelKey: "sms", CarrierTextKey: longSMS}, false},
		{"blank sms is left to the required field", map[string]interface{}{CarrierChannelKey: "sms", CarrierTextKey: ""}, true},
		{"sms with a campaign slot no run fills", map[string]interface{}{CarrierChannelKey: "sms", CarrierTextKey: "Oi {{1}}"}, false},
		{"rcs with a campaign slot no run fills", map[string]interface{}{CarrierChannelKey: "rcs", CarrierContentKey: map[string]interface{}{"text": "Oi {{1}}"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCarrierSends(carrierGraph(tc.config), everyChannelLaunched)
			if tc.ok && err != nil {
				t.Fatalf("err = %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrCarrierSendInvalid) {
				t.Fatalf("err = %v, want an invalid carrier send", err)
			}
		})
	}
}

func TestACarrierSendProblemNamesItsNodeAndField(t *testing.T) {
	err := validateCarrierSends(carrierGraph(map[string]interface{}{CarrierChannelKey: "rcs", CarrierContentKey: map[string]interface{}{
		"text": "Oi", "suggestions": []interface{}{map[string]interface{}{"kind": "reply", "label": "Sim"}, map[string]interface{}{"kind": "reply", "label": "Sim"}},
	}}), everyChannelLaunched)
	if nodeIDFromErr(err) != "send" || fieldFromErr(err) != CarrierContentKey {
		t.Fatalf("node %q field %q from %v", nodeIDFromErr(err), fieldFromErr(err), err)
	}
}

func TestLintBlocksAnInvalidCarrierMessage(t *testing.T) {
	report := LintGraph(carrierGraph(map[string]interface{}{CarrierChannelKey: "sms", CarrierTextKey: strings.Repeat("a", 160*9)}), WorkflowTypeMessages, nil, nil, nil)
	wantField := CarrierTextKey
	if !carriermessaging.ChannelSMS.Launched() {
		wantField = CarrierChannelKey
	}
	for _, issue := range report.Issues {
		if issue.Code == LintInvalidCarrierMessage && issue.NodeID == "send" && issue.Severity == SeverityBlocking && issue.Field == wantField {
			return
		}
	}
	t.Fatalf("issues = %+v", report.Issues)
}

func everyChannelLaunched(carriermessaging.Channel) bool { return true }

func TestCarrierSendsOnAChannelNotLaunchedAreRefused(t *testing.T) {
	rcsNode := map[string]interface{}{CarrierChannelKey: "rcs", CarrierContentKey: rcsContent(map[string]interface{}{"text": "Oi"})}
	if err := ValidateCarrierSends(carrierGraph(rcsNode)); carriermessaging.ChannelRCS.Launched() != (err == nil) {
		t.Fatalf("RCS node = %v while RCS launched is %v", err, carriermessaging.ChannelRCS.Launched())
	}
	if err := ValidateCarrierSends(carrierGraph(map[string]interface{}{CarrierChannelKey: "sms", CarrierTextKey: "Oi"})); carriermessaging.ChannelSMS.Launched() != (err == nil) {
		t.Fatalf("SMS node = %v while SMS launched is %v", err, carriermessaging.ChannelSMS.Launched())
	}
}
