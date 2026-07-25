package node_executors

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/workflow"
	email_usecase "vozko/usecases/email"
)

type sendEmailNodeSenderMock struct {
	calls   int
	config  email_usecase.SMTPConfig
	message email_usecase.EmailMessage
	result  *email_usecase.SendResult
	err     error
}

func (m *sendEmailNodeSenderMock) Send(_ context.Context, config email_usecase.SMTPConfig, message email_usecase.EmailMessage) (*email_usecase.SendResult, error) {
	m.calls++
	m.config = config
	m.message = message
	if m.err != nil {
		return nil, m.err
	}
	if m.result != nil {
		return m.result, nil
	}
	return &email_usecase.SendResult{To: message.To, Cc: message.Cc, Bcc: message.Bcc, Subject: message.Subject, MessageID: "mail-1", ServerResponse: "250 ok"}, nil
}

func validSendEmailNodeConfig() map[string]interface{} {
	return map[string]interface{}{
		"smtp_host":       "smtp.example.com",
		"smtp_port":       587,
		"smtp_security":   "starttls",
		"smtp_username":   "user@example.com",
		"smtp_password":   "secret",
		"smtp_from_email": "user@example.com",
		"smtp_from_name":  "ACME",
		"to":              "{{email}}",
		"cc":              "ops@example.com",
		"bcc":             "audit@example.com",
		"subject":         "Olá {{name}}",
		"body":            "<p>Olá {{name}}</p>",
		"body_type":       "html",
	}
}

func newSendEmailNodeCtx(config map[string]interface{}) *workflow.NodeContext {
	state := workflow.NewRunState()
	state.Set("email", "ana@example.com")
	state.Set("name", "Ana")
	return &workflow.NodeContext{
		Run: &workflow.WorkflowRun{ID: "run-1", WorkspaceID: "ws-1"},
		Node: &workflow.Node{
			ID:     "email-1",
			Type:   workflow.NodeTypeActionSendEmail,
			Config: config,
		},
		Graph: &workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "email-1", Type: workflow.NodeTypeActionSendEmail, Config: config},
				{ID: "success-next", Type: workflow.NodeTypeEnd},
				{ID: "failure-next", Type: workflow.NodeTypeEnd},
				{ID: "timeout-next", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{
				{Source: "email-1", Target: "success-next", Label: "sucesso"},
				{Source: "email-1", Target: "failure-next", Label: "falha"},
				{Source: "email-1", Target: "timeout-next", Label: "timeout"},
			},
		},
		State: &state,
	}
}

func TestSendEmailExecutor_Definition(t *testing.T) {
	exec := NewSendEmailExecutor(&sendEmailNodeSenderMock{})
	def := exec.Definition()
	if def.Type != workflow.NodeTypeActionSendEmail {
		t.Fatalf("unexpected node type %s", def.Type)
	}
	if def.Category != workflow.NodeCategoryMessaging {
		t.Fatalf("expected messaging category, got %s", def.Category)
	}
	if def.DefaultConfig["smtp_security"] != string(email_usecase.SMTPSecurityStartTLS) {
		t.Fatalf("unexpected defaults: %+v", def.DefaultConfig)
	}
	if len(def.Outputs) != 3 {
		t.Fatalf("expected success/failure/timeout outputs, got %d", len(def.Outputs))
	}
}

func TestSendEmailExecutor_SuccessInterpolatesAndSends(t *testing.T) {
	sender := &sendEmailNodeSenderMock{}
	exec := NewSendEmailExecutor(sender)

	result, err := exec.Execute(newSendEmailNodeCtx(validSendEmailNodeConfig()))
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if sender.calls != 1 {
		t.Fatalf("expected one send, got %d", sender.calls)
	}
	if sender.config.Host != "smtp.example.com" || sender.config.FromEmail != "user@example.com" {
		t.Fatalf("unexpected SMTP config: %+v", sender.config)
	}
	if sender.message.To[0] != "ana@example.com" || sender.message.Subject != "Olá Ana" || sender.message.Body != "<p>Olá Ana</p>" {
		t.Fatalf("unexpected message: %+v", sender.message)
	}
	if result.NextNodeID != "success-next" || result.Output["sent"] != true || result.Output["next_edge"] != "sucesso" {
		t.Fatalf("unexpected success result: %+v", result)
	}
	if result.Output["message_id"] != "mail-1" {
		t.Fatalf("expected message_id output, got %+v", result.Output)
	}
}

func TestSendEmailExecutor_InvalidConfigRoutesFailure(t *testing.T) {
	sender := &sendEmailNodeSenderMock{}
	exec := NewSendEmailExecutor(sender)
	config := validSendEmailNodeConfig()
	delete(config, "smtp_host")

	result, err := exec.Execute(newSendEmailNodeCtx(config))
	if err != nil {
		t.Fatalf("expected non-fatal validation result, got %v", err)
	}
	if sender.calls != 0 {
		t.Fatalf("expected no send, got %d", sender.calls)
	}
	if result.NextNodeID != "failure-next" || result.Output["sent"] != false || result.Output["next_edge"] != "falha" {
		t.Fatalf("unexpected failure result: %+v", result)
	}
}

func TestSendEmailExecutor_InvalidMessageRoutesFailure(t *testing.T) {
	sender := &sendEmailNodeSenderMock{}
	exec := NewSendEmailExecutor(sender)
	config := validSendEmailNodeConfig()
	config["to"] = "not-email"

	result, err := exec.Execute(newSendEmailNodeCtx(config))
	if err != nil {
		t.Fatalf("expected non-fatal validation result, got %v", err)
	}
	if sender.calls != 0 {
		t.Fatalf("expected no send, got %d", sender.calls)
	}
	if result.NextNodeID != "failure-next" || result.Output["success"] != false {
		t.Fatalf("unexpected failure result: %+v", result)
	}
}

func TestSendEmailExecutor_SenderErrorsRouteFailureAndTimeout(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		sender := &sendEmailNodeSenderMock{err: errors.New("smtp rejected")}
		exec := NewSendEmailExecutor(sender)
		result, err := exec.Execute(newSendEmailNodeCtx(validSendEmailNodeConfig()))
		if err != nil {
			t.Fatalf("expected non-fatal sender result, got %v", err)
		}
		if result.NextNodeID != "failure-next" || result.Output["next_edge"] != "falha" || result.Output["sent"] != false {
			t.Fatalf("unexpected failure route: %+v", result)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		sender := &sendEmailNodeSenderMock{err: context.DeadlineExceeded}
		exec := NewSendEmailExecutor(sender)
		result, err := exec.Execute(newSendEmailNodeCtx(validSendEmailNodeConfig()))
		if err != nil {
			t.Fatalf("expected non-fatal timeout result, got %v", err)
		}
		if result.NextNodeID != "timeout-next" || result.Output["next_edge"] != "timeout" {
			t.Fatalf("unexpected timeout route: %+v", result)
		}
	})
}

func TestSendEmailExecutor_RequiresContextAndSender(t *testing.T) {
	exec := NewSendEmailExecutor(&sendEmailNodeSenderMock{})
	_, err := exec.Execute(nil)
	if !errors.Is(err, workflow.ErrNodeConfigMissing) {
		t.Fatalf("expected config missing, got %v", err)
	}

	exec = NewSendEmailExecutor(nil)
	_, err = exec.Execute(newSendEmailNodeCtx(validSendEmailNodeConfig()))
	if err == nil || err.Error() != "email sender is not configured" {
		t.Fatalf("expected sender config error, got %v", err)
	}
}
