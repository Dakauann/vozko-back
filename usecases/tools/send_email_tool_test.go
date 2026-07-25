package tools_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/tools"
	email_usecase "vozko/usecases/email"
)

type sendEmailToolSenderMock struct {
	calls   int
	config  email_usecase.SMTPConfig
	message email_usecase.EmailMessage
	result  *email_usecase.SendResult
	err     error
}

func (m *sendEmailToolSenderMock) Send(_ context.Context, config email_usecase.SMTPConfig, message email_usecase.EmailMessage) (*email_usecase.SendResult, error) {
	m.calls++
	m.config = config
	m.message = message
	if m.err != nil {
		return nil, m.err
	}
	if m.result != nil {
		return m.result, nil
	}
	return &email_usecase.SendResult{To: message.To, Cc: message.Cc, Bcc: message.Bcc, Subject: message.Subject, MessageID: "msg-1", ServerResponse: "250 ok"}, nil
}

func validSendEmailToolConfig() map[string]interface{} {
	return map[string]interface{}{
		"smtp_host":       "smtp.example.com",
		"smtp_port":       587,
		"smtp_security":   "starttls",
		"smtp_username":   "user@example.com",
		"smtp_password":   "secret",
		"smtp_from_email": "user@example.com",
		"smtp_from_name":  "ACME",
	}
}

func validSendEmailToolParams() map[string]interface{} {
	return map[string]interface{}{
		"to":        "customer@example.com",
		"cc":        "ops@example.com",
		"bcc":       "audit@example.com",
		"subject":   "Pedido confirmado",
		"body":      "<p>Confirmado</p>",
		"body_type": "html",
	}
}

func TestSendEmailTool_DefinitionRequiresSMTPConfig(t *testing.T) {
	tool := NewSendEmailToolUseCase(&sendEmailToolSenderMock{})
	def := tool.Definition()
	if def.Name != SendEmailToolName {
		t.Fatalf("unexpected tool name %q", def.Name)
	}
	if !def.RequiresConfig {
		t.Fatal("expected send_email to require config")
	}
	if def.Category != tools.CategoryMessaging {
		t.Fatalf("expected messaging category, got %s", def.Category)
	}
	if err := def.ValidateConfig(map[string]interface{}{"smtp_host": "smtp.example.com", "smtp_from_email": "user@example.com"}); err != nil {
		t.Fatalf("expected minimal config to validate, got %v", err)
	}
	if err := def.ValidateConfig(map[string]interface{}{"smtp_host": ""}); err == nil {
		t.Fatal("expected missing required config to fail")
	}
}

func TestSendEmailTool_ExecuteRequiresConfig(t *testing.T) {
	sender := &sendEmailToolSenderMock{}
	tool := NewSendEmailToolUseCase(sender)

	res, err := tool.Execute(context.Background(), validSendEmailToolParams())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected config-required tool error, got %+v", res)
	}
	if sender.calls != 0 {
		t.Fatalf("expected no SMTP send without config, got %d", sender.calls)
	}
}

func TestSendEmailTool_ExecuteWithConfigSendsViaSMTP(t *testing.T) {
	sender := &sendEmailToolSenderMock{}
	tool := NewSendEmailToolUseCase(sender)

	res, err := tool.ExecuteWithConfig(context.Background(), validSendEmailToolConfig(), validSendEmailToolParams())
	if err != nil || res.IsError {
		t.Fatalf("expected success, got res=%+v err=%v", res, err)
	}
	if sender.calls != 1 {
		t.Fatalf("expected one send, got %d", sender.calls)
	}
	if sender.config.Host != "smtp.example.com" || sender.config.FromEmail != "user@example.com" {
		t.Fatalf("unexpected smtp config: %+v", sender.config)
	}
	if sender.message.To[0] != "customer@example.com" || sender.message.Cc[0] != "ops@example.com" || sender.message.Bcc[0] != "audit@example.com" {
		t.Fatalf("unexpected message recipients: %+v", sender.message)
	}
	result, ok := res.Result.(map[string]interface{})
	if !ok || result["sent"] != true || result["message_id"] != "msg-1" {
		t.Fatalf("unexpected result payload: %+v", res.Result)
	}
	if res.ContextUpdateText == "" {
		t.Fatal("expected context update text")
	}
}

func TestSendEmailTool_ExecuteWithConfigReturnsToolErrors(t *testing.T) {
	t.Run("invalid config", func(t *testing.T) {
		sender := &sendEmailToolSenderMock{}
		tool := NewSendEmailToolUseCase(sender)
		res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{"smtp_host": "smtp.example.com"}, validSendEmailToolParams())
		if err != nil || !res.IsError || sender.calls != 0 {
			t.Fatalf("expected invalid config tool error, got res=%+v err=%v calls=%d", res, err, sender.calls)
		}
	})

	t.Run("invalid params", func(t *testing.T) {
		sender := &sendEmailToolSenderMock{}
		tool := NewSendEmailToolUseCase(sender)
		params := validSendEmailToolParams()
		params["to"] = "not-email"
		res, err := tool.ExecuteWithConfig(context.Background(), validSendEmailToolConfig(), params)
		if err != nil || !res.IsError || sender.calls != 0 {
			t.Fatalf("expected invalid params tool error, got res=%+v err=%v calls=%d", res, err, sender.calls)
		}
	})

	t.Run("sender error", func(t *testing.T) {
		sender := &sendEmailToolSenderMock{err: errors.New("smtp rejected")}
		tool := NewSendEmailToolUseCase(sender)
		res, err := tool.ExecuteWithConfig(context.Background(), validSendEmailToolConfig(), validSendEmailToolParams())
		if err != nil || !res.IsError || sender.calls != 1 {
			t.Fatalf("expected sender tool error, got res=%+v err=%v calls=%d", res, err, sender.calls)
		}
	})
}

func TestRedactToolConfig(t *testing.T) {
	config := map[string]interface{}{
		"smtp_password": "secret",
		"api_key":       "key",
		"smtp_host":     "smtp.example.com",
	}
	redacted := redactToolConfig(config)
	if redacted["smtp_password"] != "[REDACTED]" || redacted["api_key"] != "[REDACTED]" {
		t.Fatalf("expected sensitive values to be redacted: %+v", redacted)
	}
	if redacted["smtp_host"] != "smtp.example.com" {
		t.Fatalf("expected non-sensitive value preserved: %+v", redacted)
	}
	if config["smtp_password"] != "secret" {
		t.Fatalf("redaction mutated original config: %+v", config)
	}
	if redactToolConfig(nil) != nil {
		t.Fatal("expected nil config to remain nil")
	}
}
