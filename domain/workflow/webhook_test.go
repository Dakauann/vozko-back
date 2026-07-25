package workflow

import (
	"errors"
	"testing"
)

func TestWebhookAuthModeValid(t *testing.T) {
	valid := []WebhookAuthMode{WebhookAuthNone, WebhookAuthHeaderToken, WebhookAuthHMAC}
	for _, m := range valid {
		if !m.Valid() {
			t.Errorf("expected %q to be valid", m)
		}
	}
	if WebhookAuthMode("bogus").Valid() {
		t.Error("expected bogus to be invalid")
	}
}

func baseWebhook() *WorkflowWebhook {
	return &WorkflowWebhook{
		WorkflowID:  "wf1",
		WorkspaceID: "ws1",
		Token:       "tok",
		AuthMode:    WebhookAuthHeaderToken,
		Secret:      "s",
		HeaderName:  "X-Token",
		Method:      "POST",
		Active:      true,
	}
}

func TestWorkflowWebhookValidate(t *testing.T) {
	if err := baseWebhook().Validate(); err != nil {
		t.Fatalf("valid header_token webhook: %v", err)
	}

	hmacWh := baseWebhook()
	hmacWh.AuthMode = WebhookAuthHMAC
	if err := hmacWh.Validate(); err != nil {
		t.Fatalf("valid hmac webhook: %v", err)
	}

	none := baseWebhook()
	none.AuthMode = WebhookAuthNone
	none.Secret = ""
	none.HeaderName = ""
	if err := none.Validate(); err != nil {
		t.Fatalf("valid none webhook: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*WorkflowWebhook)
		wantErr error
	}{
		{"missing workflow", func(w *WorkflowWebhook) { w.WorkflowID = "" }, ErrWorkflowIDRequired},
		{"missing workspace", func(w *WorkflowWebhook) { w.WorkspaceID = "" }, ErrWorkspaceIDRequired},
		{"missing token", func(w *WorkflowWebhook) { w.Token = "" }, ErrWebhookTokenRequired},
		{"invalid mode", func(w *WorkflowWebhook) { w.AuthMode = "bogus" }, ErrWebhookInvalidAuthMode},
		{"missing secret", func(w *WorkflowWebhook) { w.Secret = "" }, ErrWebhookSecretRequired},
		{"missing header", func(w *WorkflowWebhook) { w.HeaderName = "" }, ErrWebhookHeaderRequired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wh := baseWebhook()
			c.mutate(wh)
			if err := wh.Validate(); !errors.Is(err, c.wantErr) {
				t.Fatalf("got %v, want %v", err, c.wantErr)
			}
		})
	}
}

func TestWorkflowWebhookAllowsMethod(t *testing.T) {
	wh := baseWebhook()
	wh.Method = "POST"
	if !wh.AllowsMethod("post") {
		t.Error("expected case-insensitive POST match")
	}
	if wh.AllowsMethod("GET") {
		t.Error("expected GET to be rejected")
	}

	wh.Method = ""
	if !wh.AllowsMethod("POST") {
		t.Error("expected empty method to default to POST")
	}
	if wh.AllowsMethod("PUT") {
		t.Error("expected PUT rejected against default POST")
	}
}
