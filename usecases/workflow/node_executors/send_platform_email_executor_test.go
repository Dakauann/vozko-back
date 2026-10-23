package node_executors

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	es "vozko/domain/emailsending"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/usecases/transactionalemail"
)

type recordingPlatformEmail struct {
	requests []transactionalemail.Request
	result   transactionalemail.Result
	err      error
}

func (r *recordingPlatformEmail) Send(_ context.Context, req transactionalemail.Request) (transactionalemail.Result, error) {
	r.requests = append(r.requests, req)
	if r.err != nil {
		return transactionalemail.Result{}, r.err
	}
	return r.result, nil
}

type failingEntryLeads struct{}

func (failingEntryLeads) LeadOfEntry(context.Context, string, shared.EntryRef) (string, error) {
	return "", errors.New("db down")
}

func platformEmailContext(run *workflow.WorkflowRun, config map[string]interface{}) *workflow.NodeContext {
	node := &workflow.Node{ID: "mail", Type: workflow.NodeTypeActionSendPlatformEmail, Config: config}
	graph := &workflow.Graph{Nodes: []workflow.Node{*node}, Edges: []workflow.Edge{
		{Source: "mail", Target: "to_sent", Label: workflow.PlatformEmailHandleSent},
		{Source: "mail", Target: "to_failed", Label: workflow.PlatformEmailHandleFailed},
	}}
	state := workflow.NewRunState()
	state.Set("lead_name", "Ana")
	state.Set("pedido", "A-17")
	state.Set(workflow.StateKeyDurableSteps, 4)
	run.CurrentNodeID = node.ID
	run.State = state
	return &workflow.NodeContext{Run: run, Node: node, Graph: graph, State: &run.State, Workflow: &workflow.Workflow{ID: "wf-1"}}
}

func platformTextConfig() map[string]interface{} {
	return map[string]interface{}{
		workflow.PlatformEmailSubjectKey:  "Pedido {{pedido}}",
		workflow.PlatformEmailBodyModeKey: workflow.PlatformEmailBodyText,
		workflow.PlatformEmailTextKey:     "Oi {{lead_name}}, seu pedido {{pedido}} saiu.",
	}
}

func TestSendPlatformEmailSendsToTheLeadOfALeadRun(t *testing.T) {
	sender := &recordingPlatformEmail{result: transactionalemail.Result{MessageID: "m-1", ProviderMessageID: "p-1", To: "ana@acme.com.br"}}
	exec := NewSendPlatformEmailExecutor(PlatformEmailNodeDeps{Sender: sender})
	result, err := exec.Execute(platformEmailContext(leadRun(), platformTextConfig()))
	if err != nil {
		t.Fatal(err)
	}
	req := sender.requests[0]
	if req.WorkspaceID != "ws-1" || req.LeadID != "lead-1" || req.To != "" || req.SenderID != "" || req.Origin != transactionalemail.OriginWorkflow || req.SentBy != conversation.SentByWorkflow("wf-1") {
		t.Fatalf("request = %+v", req)
	}
	if req.Subject != "Pedido A-17" || req.Body.Text != "Oi Ana, seu pedido A-17 saiu." || req.Body.DesignID != "" {
		t.Fatalf("content = %+v", req)
	}
	if req.IdempotencyKey != "workflow:run-1:mail:mail#4" {
		t.Fatalf("idempotency key = %q", req.IdempotencyKey)
	}
	if result.NextNodeID != "to_sent" || result.Output["sent"] != true || result.Output["message_id"] != "m-1" || result.Output["to"] != "ana@acme.com.br" || result.Output["repeated"] != false {
		t.Fatalf("result = %+v", result)
	}
}

func TestSendPlatformEmailKeyFollowsTheVisitNotTheRetry(t *testing.T) {
	sender := &recordingPlatformEmail{}
	exec := NewSendPlatformEmailExecutor(PlatformEmailNodeDeps{Sender: sender})
	first := platformEmailContext(leadRun(), platformTextConfig())
	retry := platformEmailContext(leadRun(), platformTextConfig())
	retry.Run.RetryCount = 1
	retry.State.Set(workflow.StateKeyDurableSteps, 5)
	nextLoop := platformEmailContext(leadRun(), platformTextConfig())
	nextLoop.State.Set(workflow.StateKeyDurableSteps, 9)
	for _, ctx := range []*workflow.NodeContext{first, retry, nextLoop} {
		if _, err := exec.Execute(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if sender.requests[0].IdempotencyKey != sender.requests[1].IdempotencyKey {
		t.Fatalf("a retry must reuse the key: %q vs %q", sender.requests[0].IdempotencyKey, sender.requests[1].IdempotencyKey)
	}
	if sender.requests[0].IdempotencyKey == sender.requests[2].IdempotencyKey {
		t.Fatal("another visit of the node is another email")
	}
}

func TestSendPlatformEmailUsesTheDesignSenderAndRecipient(t *testing.T) {
	sender := &recordingPlatformEmail{}
	run := &workflow.WorkflowRun{ID: "run-2", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "entry-9", EntryType: "whatsapp"}
	config := map[string]interface{}{
		workflow.PlatformEmailSenderKey:   "snd-1",
		workflow.PlatformEmailToKey:       "{{lead_name}}@acme.com.br",
		workflow.PlatformEmailSubjectKey:  "Oi",
		workflow.PlatformEmailBodyModeKey: workflow.PlatformEmailBodyDesign,
		workflow.PlatformEmailDesignKey:   "d-1",
		workflow.PlatformEmailTextKey:     "ignored",
	}
	exec := NewSendPlatformEmailExecutor(PlatformEmailNodeDeps{Sender: sender, EntryLeads: carrierEntryLeads{"entry-9": "lead-7"}})
	if _, err := exec.Execute(platformEmailContext(run, config)); err != nil {
		t.Fatal(err)
	}
	req := sender.requests[0]
	if req.SenderID != "snd-1" || req.To != "Ana@acme.com.br" || req.LeadID != "lead-7" || req.Body.DesignID != "d-1" || req.Body.Text != "" {
		t.Fatalf("request = %+v", req)
	}
}

func TestSendPlatformEmailFailsThroughTheFailedHandle(t *testing.T) {
	cases := []struct {
		name   string
		deps   PlatformEmailNodeDeps
		run    *workflow.WorkflowRun
		config map[string]interface{}
		reason string
	}{
		{"no sender wired", PlatformEmailNodeDeps{}, leadRun(), platformTextConfig(), transactionalemail.CodeUnavailable},
		{"unreadable config", PlatformEmailNodeDeps{Sender: &recordingPlatformEmail{}}, leadRun(), map[string]interface{}{workflow.PlatformEmailBodyModeKey: "pdf"}, PlatformEmailReasonConfigUnreadable},
		{"refused send", PlatformEmailNodeDeps{Sender: &recordingPlatformEmail{err: &transactionalemail.Refusal{Code: transactionalemail.CodeLeadBlocked, Err: transactionalemail.ErrLeadBlocked}}}, leadRun(), platformTextConfig(), transactionalemail.CodeLeadBlocked},
		{"unreadable lead", PlatformEmailNodeDeps{Sender: &recordingPlatformEmail{}, EntryLeads: failingEntryLeads{}}, &workflow.WorkflowRun{ID: "r", WorkspaceID: "ws-1", EntryID: "e", EntryType: "whatsapp"}, platformTextConfig(), PlatformEmailReasonLeadUnreadable},
	}
	for _, tc := range cases {
		result, err := NewSendPlatformEmailExecutor(tc.deps).Execute(platformEmailContext(tc.run, tc.config))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if result.NextNodeID != "to_failed" || result.Output["sent"] != false || result.Output["reason"] != tc.reason || result.Output["error"] == "" {
			t.Errorf("%s: result = %+v", tc.name, result)
		}
	}
}

func TestSendPlatformEmailDefinition(t *testing.T) {
	def := NewSendPlatformEmailExecutor(PlatformEmailNodeDeps{}).Definition()
	if def.Type != workflow.NodeTypeActionSendPlatformEmail || def.Label != "Enviar e-mail (plataforma)" || def.Category != workflow.NodeCategoryMessaging {
		t.Fatalf("definition = %+v", def)
	}
	if len(def.Scopes) != 1 || def.Scopes[0] != workflow.NodeScopeShared || def.TestBehavior != workflow.NodeTestExternalCall {
		t.Fatalf("scopes = %v, behaviour = %s", def.Scopes, def.TestBehavior)
	}
	if err := workflow.TestBehaviorConsistent(def); err != nil {
		t.Fatal(err)
	}
	fields := map[string]workflow.ConfigField{}
	for _, f := range def.ConfigSchema {
		fields[f.Key] = f
	}
	if !fields[workflow.PlatformEmailSubjectKey].Required || fields[workflow.PlatformEmailSenderKey].Required || fields[workflow.PlatformEmailSenderKey].OptionsSource != "email_senders" {
		t.Fatalf("subject and sender fields = %+v", fields)
	}
	text, design := fields[workflow.PlatformEmailTextKey], fields[workflow.PlatformEmailDesignKey]
	if !text.RequiredFor(map[string]interface{}{workflow.PlatformEmailBodyModeKey: "text"}) || text.RequiredFor(map[string]interface{}{workflow.PlatformEmailBodyModeKey: "design"}) {
		t.Fatal("the text is required in text mode only")
	}
	if !design.RequiredFor(map[string]interface{}{workflow.PlatformEmailBodyModeKey: "design"}) || design.OptionsSource != "email_designs" {
		t.Fatal("the design is required in design mode and picked from the saved designs")
	}
	handles := map[string]bool{}
	for _, h := range def.Outputs {
		handles[h.ID] = true
	}
	if !handles[workflow.PlatformEmailHandleSent] || !handles[workflow.PlatformEmailHandleFailed] {
		t.Fatalf("outputs = %+v", def.Outputs)
	}
}

func throttledPlatformEmail() *recordingPlatformEmail {
	return &recordingPlatformEmail{err: &transactionalemail.Refusal{
		Code: transactionalemail.CodeSendThrottled,
		Err:  &es.SendError{Kind: es.SendErrorTransient, Detail: "pacing: rate", RetryAfter: 1500 * time.Millisecond},
	}}
}

func TestSendPlatformEmailRetriesAThrottledSendWithTheSameKey(t *testing.T) {
	sender := throttledPlatformEmail()
	exec := NewSendPlatformEmailExecutor(PlatformEmailNodeDeps{Sender: sender})
	run := leadRun()
	ctx := platformEmailContext(run, platformTextConfig())
	result, err := exec.Execute(ctx)
	if err == nil || result != nil || transactionalemail.CodeOf(err) != transactionalemail.CodeSendThrottled {
		t.Fatalf("a throttled send is handed to the engine's retry: result %+v err %v", result, err)
	}
	run.RetryCount = 1
	retry := platformEmailContext(run, platformTextConfig())
	retry.State.Set(workflow.StateKeyDurableSteps, 5)
	if _, err := exec.Execute(retry); err == nil {
		t.Fatal("a retry that is still throttled retries again")
	}
	if sender.requests[0].IdempotencyKey != sender.requests[1].IdempotencyKey {
		t.Fatalf("the engine retry keeps the key: %q vs %q", sender.requests[0].IdempotencyKey, sender.requests[1].IdempotencyKey)
	}
}

func TestSendPlatformEmailFailsAThrottledSendWhenItCannotWait(t *testing.T) {
	spent := leadRun()
	spent.RetryCount = workflow.MaxRetriesPerNode
	cases := []struct {
		name string
		ctx  func() *workflow.NodeContext
	}{
		{"retries spent", func() *workflow.NodeContext { return platformEmailContext(spent, platformTextConfig()) }},
		{"a caller on the line", func() *workflow.NodeContext {
			ctx := platformEmailContext(leadRun(), platformTextConfig())
			ctx.Runtime = &fakeVoiceCall{ended: make(chan struct{})}
			return ctx
		}},
	}
	for _, tc := range cases {
		result, err := NewSendPlatformEmailExecutor(PlatformEmailNodeDeps{Sender: throttledPlatformEmail()}).Execute(tc.ctx())
		if err != nil || result.NextNodeID != "to_failed" || result.Output["reason"] != transactionalemail.CodeSendThrottled || result.Output["retry_after_seconds"] != int64(2) {
			t.Errorf("%s: result %+v err %v", tc.name, result, err)
		}
	}
}
