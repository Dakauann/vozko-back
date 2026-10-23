package tools_usecase

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	es "vozko/domain/emailsending"
	"vozko/domain/tools"
	"vozko/usecases/transactionalemail"
)

type platformEmailSenderFake struct {
	requests []transactionalemail.Request
	result   transactionalemail.Result
	err      error
}

func (f *platformEmailSenderFake) Send(_ context.Context, req transactionalemail.Request) (transactionalemail.Result, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return transactionalemail.Result{}, f.err
	}
	return f.result, nil
}

func platformEmailSeed() map[string]interface{} {
	return map[string]interface{}{"__workspace_id": "ws-1", "__entry_id": "entry-1", "__lead_id": "lead-1", "__agent_id": "agent-1"}
}

func TestSendPlatformEmailToolDefinition(t *testing.T) {
	def := NewSendPlatformEmailTool(&platformEmailSenderFake{}).Definition()
	if SendPlatformEmailToolName != "send_platform_email" || def.Name != SendPlatformEmailToolName || def.RequiresConfig || def.Category != tools.CategoryMessaging {
		t.Fatalf("definition = %+v", def)
	}
	if len(def.Required) != 1 || def.Required[0] != "subject" {
		t.Fatalf("required = %v", def.Required)
	}
	for _, name := range []string{"to", "subject", "text", "design_id"} {
		if _, ok := def.Parameters[name]; !ok {
			t.Fatalf("missing parameter %s", name)
		}
	}
}

func TestSendPlatformEmailToolSendsToTheLeadOfTheConversation(t *testing.T) {
	sender := &platformEmailSenderFake{result: transactionalemail.Result{MessageID: "m-1", To: "ana@acme.com.br"}}
	tool := NewSendPlatformEmailTool(sender)
	ctx := tools.WithCallID(context.Background(), "call_7")
	params := map[string]interface{}{"subject": "Seu boleto", "text": "Oi {{first_name}}, segue."}
	result, err := tool.ExecuteWithConfig(ctx, platformEmailSeed(), params)
	if err != nil || result.IsError {
		t.Fatalf("result = %+v, %v", result, err)
	}
	req := sender.requests[0]
	if req.WorkspaceID != "ws-1" || req.LeadID != "lead-1" || req.To != "" || req.Subject != "Seu boleto" || req.Body.Text != "Oi {{first_name}}, segue." || req.Origin != transactionalemail.OriginAgent || req.SentBy != conversation.SentByAI("agent-1") {
		t.Fatalf("request = %+v", req)
	}
	if !strings.HasPrefix(req.IdempotencyKey, "agent:entry-1:call_7:") {
		t.Fatalf("key = %q", req.IdempotencyKey)
	}
	data, ok := result.Result.(map[string]interface{})
	if !ok || data["sent"] != true || data["message_id"] != "m-1" || data["to"] != "ana@acme.com.br" {
		t.Fatalf("result = %+v", result.Result)
	}

	again, _ := tool.ExecuteWithConfig(ctx, platformEmailSeed(), params)
	other, _ := tool.ExecuteWithConfig(ctx, platformEmailSeed(), map[string]interface{}{"subject": "Outro", "text": "Outro texto"})
	if again.IsError || other.IsError || sender.requests[1].IdempotencyKey != req.IdempotencyKey || sender.requests[2].IdempotencyKey == req.IdempotencyKey {
		t.Fatal("the same call repeats the same key; another email in the same call id gets another key")
	}
}

func TestSendPlatformEmailToolUsesADesignAndAnAddress(t *testing.T) {
	sender := &platformEmailSenderFake{}
	tool := NewSendPlatformEmailTool(sender)
	params := map[string]interface{}{"subject": "Oi", "design_id": "d-1", "to": "outro@acme.com.br"}
	if result, _ := tool.ExecuteWithConfig(context.Background(), platformEmailSeed(), params); result.IsError {
		t.Fatalf("result = %+v", result)
	}
	req := sender.requests[0]
	if req.Body.DesignID != "d-1" || req.Body.Text != "" || req.To != "outro@acme.com.br" || req.IdempotencyKey == "" {
		t.Fatalf("request = %+v", req)
	}
}

func TestSendPlatformEmailToolRefusals(t *testing.T) {
	refused := &platformEmailSenderFake{err: &transactionalemail.Refusal{Code: transactionalemail.CodeLeadBlocked, Err: transactionalemail.ErrLeadBlocked}}
	cases := []struct {
		name   string
		sender transactionalemail.Sender
		config map[string]interface{}
		params map[string]interface{}
		want   string
	}{
		{"no workspace", &platformEmailSenderFake{}, map[string]interface{}{}, map[string]interface{}{"subject": "Oi", "text": "x"}, "workspace"},
		{"no body", &platformEmailSenderFake{}, platformEmailSeed(), map[string]interface{}{"subject": "Oi"}, "text"},
		{"both bodies", &platformEmailSenderFake{}, platformEmailSeed(), map[string]interface{}{"subject": "Oi", "text": "x", "design_id": "d"}, "text"},
		{"refused", refused, platformEmailSeed(), map[string]interface{}{"subject": "Oi", "text": "x"}, transactionalemail.CodeLeadBlocked},
		{"unwired", nil, platformEmailSeed(), map[string]interface{}{"subject": "Oi", "text": "x"}, transactionalemail.CodeUnavailable},
	}
	for _, tc := range cases {
		result, err := NewSendPlatformEmailTool(tc.sender).ExecuteWithConfig(context.Background(), tc.config, tc.params)
		text, _ := result.Result.(string)
		if err != nil || !result.IsError || !strings.Contains(text, tc.want) {
			t.Errorf("%s: result = %+v, %v", tc.name, result, err)
		}
	}
	if result, _ := NewSendPlatformEmailTool(&platformEmailSenderFake{}).Execute(context.Background(), map[string]interface{}{"subject": "Oi", "text": "x"}); !result.IsError {
		t.Fatal("without the conversation seed the tool cannot know the workspace")
	}
}

func TestSendPlatformEmailToolTellsTheAgentAThrottledSendCanBeRetried(t *testing.T) {
	throttled := &platformEmailSenderFake{err: &transactionalemail.Refusal{
		Code: transactionalemail.CodeSendThrottled,
		Err:  &es.SendError{Kind: es.SendErrorTransient, Detail: "pacing: rate", RetryAfter: 2 * time.Second},
	}}
	result, err := NewSendPlatformEmailTool(throttled).ExecuteWithConfig(context.Background(), platformEmailSeed(), map[string]interface{}{"subject": "Oi", "text": "x"})
	text, _ := result.Result.(string)
	if err != nil || !result.IsError || result.ErrorCode != transactionalemail.CodeSendThrottled ||
		!strings.Contains(text, "temporário") || !strings.Contains(text, "2 segundos") || !strings.Contains(text, "nada foi cobrado") {
		t.Fatalf("result = %+v, %v", result, err)
	}

	blocked := &platformEmailSenderFake{err: &transactionalemail.Refusal{Code: transactionalemail.CodeLeadBlocked, Err: transactionalemail.ErrLeadBlocked}}
	result, _ = NewSendPlatformEmailTool(blocked).ExecuteWithConfig(context.Background(), platformEmailSeed(), map[string]interface{}{"subject": "Oi", "text": "x"})
	if text, _ := result.Result.(string); result.ErrorCode != transactionalemail.CodeLeadBlocked || strings.Contains(text, "temporário") {
		t.Fatalf("a permanent refusal is not offered as retryable: %+v", result)
	}
}
