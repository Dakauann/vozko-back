package workflow

import (
	"errors"
	"testing"
)

func platformEmailGraph(config map[string]interface{}) *Graph {
	return &Graph{
		Nodes: []Node{
			{ID: "t", Type: NodeTypeTriggerMessageReceived},
			{ID: "mail", Type: NodeTypeActionSendPlatformEmail, Config: config},
			{ID: "e", Type: NodeTypeEnd},
		},
		Edges: []Edge{{Source: "t", Target: "mail"}, {Source: "mail", Target: "e", Label: PlatformEmailHandleSent}},
	}
}

func TestSendPlatformEmailIsAMessagingNode(t *testing.T) {
	n := NodeTypeActionSendPlatformEmail
	if n != "action_send_platform_email" || !n.Valid() || !n.IsMessaging() || n.Category() != NodeCategoryMessaging {
		t.Fatalf("node type %q must be a valid messaging node", n)
	}
	if !ExternalCallNode(Node{Type: n}) {
		t.Fatal("the platform email leaves the platform and must ask before a test run sends it")
	}
}

func TestReadPlatformEmailSend(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		want   PlatformEmailSend
		err    error
	}{
		{
			name:   "text with defaults",
			config: map[string]interface{}{"subject": "Oi", "text": "Corpo"},
			want:   PlatformEmailSend{Subject: "Oi", Mode: PlatformEmailBodyText, Text: "Corpo"},
		},
		{
			name: "design with sender and recipient",
			config: map[string]interface{}{
				"sender_id": " s-1 ", "to": "{{email}}", "subject": "Oi", "body_mode": "design", "design_id": " d-1 ", "text": "ignored",
			},
			want: PlatformEmailSend{SenderID: "s-1", To: "{{email}}", Subject: "Oi", Mode: PlatformEmailBodyDesign, DesignID: "d-1", Text: "ignored"},
		},
		{name: "unknown mode", config: map[string]interface{}{"body_mode": "pdf"}, err: ErrPlatformEmailUnreadable},
		{name: "subject not text", config: map[string]interface{}{"subject": 3}, err: ErrPlatformEmailUnreadable},
		{name: "design not text", config: map[string]interface{}{"body_mode": "design", "design_id": []interface{}{"d"}}, err: ErrPlatformEmailUnreadable},
	}
	for _, tc := range cases {
		got, err := ReadPlatformEmailSend(tc.config)
		if !errors.Is(err, tc.err) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.err)
			continue
		}
		if tc.err == nil && got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestPlatformEmailRender(t *testing.T) {
	send := PlatformEmailSend{To: "{{lead.email}}", Subject: "Oi {{nome}}", Text: "Pedido {{pedido}}", Mode: PlatformEmailBodyText}
	got := send.Render(func(s string) string {
		switch s {
		case "{{lead.email}}":
			return "a@b.com"
		case "Oi {{nome}}":
			return "Oi Ana"
		case "Pedido {{pedido}}":
			return "Pedido 7"
		}
		return s
	})
	if got.To != "a@b.com" || got.Subject != "Oi Ana" || got.Text != "Pedido 7" || got.Mode != PlatformEmailBodyText {
		t.Fatalf("rendered = %+v", got)
	}
}

func TestValidatePlatformEmailSends(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		field  string
	}{
		{name: "text ready", config: map[string]interface{}{"subject": "Oi", "body_mode": "text", "text": "Corpo"}},
		{name: "design ready", config: map[string]interface{}{"subject": "Oi", "body_mode": "design", "design_id": "d-1"}},
		{name: "unknown mode", config: map[string]interface{}{"subject": "Oi", "body_mode": "html"}, field: PlatformEmailBodyModeKey},
		{name: "design id is an expression", config: map[string]interface{}{"subject": "Oi", "body_mode": "design", "design_id": "{{x}}"}, field: PlatformEmailDesignKey},
		{name: "sender is an expression", config: map[string]interface{}{"subject": "Oi", "text": "Corpo", "sender_id": "{{x}}"}, field: PlatformEmailSenderKey},
	}
	for _, tc := range cases {
		err := ValidatePlatformEmailSends(platformEmailGraph(tc.config))
		if tc.field == "" {
			if err != nil {
				t.Errorf("%s: %v", tc.name, err)
			}
			continue
		}
		if !errors.Is(err, ErrPlatformEmailInvalid) || fieldFromErr(err) != tc.field || nodeIDFromErr(err) != "mail" {
			t.Errorf("%s: err = %v (field %q)", tc.name, err, fieldFromErr(err))
		}
	}
}

func TestLintFlagsAnInvalidPlatformEmail(t *testing.T) {
	g := platformEmailGraph(map[string]interface{}{"subject": "Oi", "body_mode": "html"})
	report := LintGraph(g, WorkflowTypeMessages, nil, nil, nil)
	for _, issue := range report.Issues {
		if issue.Code == LintInvalidPlatformEmail && issue.NodeID == "mail" && issue.Field == PlatformEmailBodyModeKey {
			return
		}
	}
	t.Fatalf("issues = %+v", report.Issues)
}
