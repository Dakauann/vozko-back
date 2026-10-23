package emailwiring_test

import (
	"testing"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
	"vozko/domain/tools"
	"vozko/domain/workflow"
	"vozko/infra/database/schema"
	"vozko/infra/ses/sesfake"
	"vozko/usecases/emailoutreach"
	tools_usecase "vozko/usecases/tools"
	"vozko/usecases/transactionalemail"
	"vozko/usecases/workflow/node_executors"
)

func (w *world) startOutreach(ws *tenant, lead schema.Lead, key string) (emailoutreach.Started, error) {
	w.t.Helper()
	return w.outreach.Execute(w.ctx, emailoutreach.Input{
		WorkspaceID: ws.ID, UserID: ws.Actor.UserID, IsAdmin: true, LeadID: lead.ID,
		Subject: "Seu orçamento", Text: "Oi {{first_name}}, segue o orçamento que combinamos.", IdempotencyKey: key,
	})
}

func (w *world) oneToOneCharges(ws *tenant) ([]schema.BalanceTransaction, []schema.BalanceTransaction) {
	return w.ledger(ws, "debit", "email_message:tx:%"), w.ledger(ws, "credit", "refund:email_message:tx:%")
}

func (w *world) assertOneRow(t *testing.T, conversationID, providerID string, by conversation.SentBy) {
	t.Helper()
	rows := w.transcript(conversationID)
	if len(rows) != 1 {
		t.Fatalf("conversation %s has %d rows, want exactly one", conversationID, len(rows))
	}
	row := rows[0]
	if row.ExternalMessageID != providerID || row.SenderKind != string(by.Kind()) || row.SenderID != by.ID() {
		t.Fatalf("row = %+v, want %s by %s %s", row, providerID, by.Kind(), by.ID())
	}
}

func TestEmailE2E_EveryOneToOnePathRecordsOneConversationRowAndChargesOnce(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Avulsa", roomyWallet)
	unit := w.unit(ws)
	people := w.seedLeads(ws, 3, "avulso")
	byOperator, byWorkflow, byAgent := people[0], people[1], people[2]

	first, err := w.startOutreach(ws, byOperator, "compose-1")
	w.must(err)
	again, err := w.startOutreach(ws, byOperator, "compose-1")
	w.must(err)
	if !first.Recorded || first.Repeated || !again.Repeated || !again.Recorded || again.ConversationID != first.ConversationID || again.MessageID != first.MessageID {
		t.Fatalf("outreach = %+v then %+v", first, again)
	}
	operatorSent := w.onlySentTo(emailaddress.Address(byOperator.Email))
	w.assertOneRow(t, first.ConversationID, operatorSent.ProviderMessageID, conversation.SentByPerson(ws.Actor.UserID))
	if w.assignments.claimedBy(first.ConversationID) != ws.Actor.UserID {
		t.Fatalf("the outreach conversation was not claimed for the operator")
	}

	node := node_executors.NewSendPlatformEmailExecutor(node_executors.PlatformEmailNodeDeps{Sender: w.relay})
	run := &workflow.WorkflowRun{ID: uuid.NewString(), WorkflowID: "wf-boas-vindas", WorkspaceID: ws.ID, EntryType: workflow.EntryTypeLead,
		EntryID: byWorkflow.ID, CurrentNodeID: "email-1", State: workflow.NewRunState()}
	step := &workflow.NodeContext{Run: run, State: &run.State, Node: &workflow.Node{ID: "email-1", Type: workflow.NodeTypeActionSendPlatformEmail, Config: map[string]interface{}{
		workflow.PlatformEmailSubjectKey: "Bem-vinda, {{first_name}}", workflow.PlatformEmailBodyModeKey: workflow.PlatformEmailBodyText,
		workflow.PlatformEmailTextKey: "Que bom ter você aqui.",
	}}}
	var outputs []map[string]interface{}
	for i := 0; i < 2; i++ {
		result, err := node.Execute(step)
		w.must(err)
		outputs = append(outputs, result.Output)
	}
	if outputs[0]["sent"] != true || outputs[0]["repeated"] != false || outputs[1]["repeated"] != true || outputs[0]["message_id"] != outputs[1]["message_id"] {
		t.Fatalf("node outputs = %v", outputs)
	}
	workflowSent := w.onlySentTo(emailaddress.Address(byWorkflow.Email))
	w.assertOneRow(t, w.conversationOf(ws, emailaddress.Address(byWorkflow.Email)), workflowSent.ProviderMessageID, conversation.SentByWorkflow("wf-boas-vindas"))

	tool := tools_usecase.NewSendPlatformEmailTool(w.relay)
	config := map[string]interface{}{"__workspace_id": ws.ID, "__lead_id": byAgent.ID, "__entry_id": uuid.NewString(), "__agent_id": "agente-vendas"}
	params := map[string]interface{}{"subject": "Sua proposta", "text": "Conforme conversamos, segue a proposta."}
	call := tools.WithCallID(w.ctx, "call-proposta")
	var results []tools.ExecutionResult
	for i := 0; i < 2; i++ {
		result, err := tool.ExecuteWithConfig(call, config, params)
		w.must(err)
		results = append(results, result)
	}
	if results[0].IsError || results[1].IsError {
		t.Fatalf("tool results = %+v", results)
	}
	agentSent := w.onlySentTo(emailaddress.Address(byAgent.Email))
	w.assertOneRow(t, w.conversationOf(ws, emailaddress.Address(byAgent.Email)), agentSent.ProviderMessageID, conversation.SentByAI("agente-vendas"))

	debits, refunds := w.oneToOneCharges(ws)
	if len(debits) != 3 || len(refunds) != 0 || sumOf(debits) != 3*unit {
		t.Fatalf("debits %v refunds %v, want one charge per path", amounts(debits), amounts(refunds))
	}
	if len(w.ses.Sent()) != 3 || w.conversationsOf(ws) != 3 {
		t.Fatalf("sends %d conversations %d, want three of each", len(w.ses.Sent()), w.conversationsOf(ws))
	}
	if got, want := w.wallet(ws), ws.Start-3*unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertLedger(ws)
}

func TestEmailE2E_ARecordingFailureKeepsTheChargeAndTheRetryRecordsWithoutResending(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Falha", roomyWallet)
	unit := w.unit(ws)
	ana := w.seedLeads(ws, 1, "falha")[0]

	w.history.failNext(1)
	lost, err := w.startOutreach(ws, ana, "compose-falha")
	w.must(err)
	if lost.Recorded || lost.ConversationID != "" || lost.MessageID == "" || lost.Repeated {
		t.Fatalf("a send whose transcript failed = %+v, want sent but recorded false", lost)
	}
	sent := w.onlySentTo(emailaddress.Address(ana.Email))
	if debits, refunds := w.oneToOneCharges(ws); len(debits) != 1 || len(refunds) != 0 {
		t.Fatalf("debits %v refunds %v: a transcript failure never refunds", amounts(debits), amounts(refunds))
	}
	if rows := w.transcript(w.conversationOf(ws, emailaddress.Address(ana.Email))); len(rows) != 0 {
		t.Fatalf("rows = %d before the retry", len(rows))
	}

	retried, err := w.startOutreach(ws, ana, "compose-falha")
	w.must(err)
	if !retried.Recorded || !retried.Repeated || retried.MessageID != lost.MessageID || retried.ConversationID == "" {
		t.Fatalf("retry = %+v, want the same message recorded", retried)
	}
	w.onlySentTo(emailaddress.Address(ana.Email))
	w.assertOneRow(t, retried.ConversationID, sent.ProviderMessageID, conversation.SentByPerson(ws.Actor.UserID))
	if debits, refunds := w.oneToOneCharges(ws); len(debits) != 1 || len(refunds) != 0 || debits[0].Amount != unit {
		t.Fatalf("debits %v refunds %v after the retry", amounts(debits), amounts(refunds))
	}
	if got, want := w.wallet(ws), ws.Start-unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertLedger(ws)
}

func TestEmailE2E_APacerRefusalRefundsTheOneToOneSendAndTheRetrySendsOnce(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Cota", roomyWallet)
	unit := w.unit(ws)
	ana := w.seedLeads(ws, 1, "cota")[0]
	w.ses.SetQuota(es.SendQuota{MaxSendRate: 100, Max24HourSend: 1000, SentLast24Hours: 1000, ProductionAccess: true})
	w.must(w.module.SendQuota.Execute(w.ctx))

	_, err := w.startOutreach(ws, ana, "compose-cota")
	if code := transactionalemail.CodeOf(err); code != transactionalemail.CodeSendThrottled || !transactionalemail.Retryable(err) {
		t.Fatalf("throttled send = %v (%s), want the retryable %s", err, code, transactionalemail.CodeSendThrottled)
	}
	if seconds, ok := transactionalemail.RetryAfterSeconds(err); !ok || seconds != int64(es.DailyQuotaRetryAfter.Seconds()) {
		t.Fatalf("retry hint = %d %v, want %v", seconds, ok, es.DailyQuotaRetryAfter)
	}
	node := node_executors.NewSendPlatformEmailExecutor(node_executors.PlatformEmailNodeDeps{Sender: w.relay})
	run := &workflow.WorkflowRun{ID: uuid.NewString(), WorkflowID: "wf-cota", WorkspaceID: ws.ID, EntryType: workflow.EntryTypeLead,
		EntryID: ana.ID, CurrentNodeID: "email-1", State: workflow.NewRunState()}
	result, err := node.Execute(&workflow.NodeContext{Run: run, State: &run.State, Node: &workflow.Node{ID: "email-1", Type: workflow.NodeTypeActionSendPlatformEmail, Config: map[string]interface{}{
		workflow.PlatformEmailSubjectKey: "Lembrete", workflow.PlatformEmailBodyModeKey: workflow.PlatformEmailBodyText, workflow.PlatformEmailTextKey: "Oi!",
	}}})
	if result != nil || transactionalemail.CodeOf(err) != transactionalemail.CodeSendThrottled {
		t.Fatalf("node = %+v %v, want the throttle handed to the engine for a retry with the same key", result, err)
	}
	refusal, err := tools_usecase.NewSendPlatformEmailTool(w.relay).ExecuteWithConfig(tools.WithCallID(w.ctx, "call-cota"),
		map[string]interface{}{"__workspace_id": ws.ID, "__lead_id": ana.ID, "__entry_id": uuid.NewString(), "__agent_id": "agente-cota"},
		map[string]interface{}{"subject": "Lembrete", "text": "Oi!"})
	w.must(err)
	if !refusal.IsError || refusal.ErrorCode != transactionalemail.CodeSendThrottled {
		t.Fatalf("agent tool = %+v, want a temporary refusal coded %s", refusal, transactionalemail.CodeSendThrottled)
	}
	if len(w.ses.Sent()) != 0 {
		t.Fatal("a refused send reached the provider")
	}
	debits, refunds := w.oneToOneCharges(ws)
	if len(debits) != 3 || len(refunds) != 3 || sumOf(refunds) != sumOf(debits) || w.wallet(ws) != ws.Start {
		t.Fatalf("debits %v refunds %v wallet %d: every throttled send is refunded", amounts(debits), amounts(refunds), w.wallet(ws))
	}
	runID := transactionalemail.RunID(ws.ID, "outreach:compose-cota")
	if rows, err := w.runs.ListByRun(w.ctx, ws.ID, es.RunTriggered, runID); err != nil || len(rows) != 0 {
		t.Fatalf("message rows of the refused send = %d %v, want none", len(rows), err)
	}

	w.ses.SetQuota(sesfake.UnlimitedQuota())
	w.must(w.module.SendQuota.Execute(w.ctx))
	started, err := w.startOutreach(ws, ana, "compose-cota")
	w.must(err)
	if !started.Recorded || started.Repeated {
		t.Fatalf("retry = %+v", started)
	}
	sent := w.onlySentTo(emailaddress.Address(ana.Email))
	w.assertOneRow(t, started.ConversationID, sent.ProviderMessageID, conversation.SentByPerson(ws.Actor.UserID))
	retried := w.ledger(ws, "debit", "email_message:tx:"+runID+"%")
	if len(retried) != 2 || *retried[1].ReferenceID != "email_message:tx:"+runID+"#2" || len(w.ledger(ws, "credit", "refund:email_message:tx:"+runID+"%")) != 1 {
		t.Fatalf("outreach debits %v, want the refused round refunded and the retry charged once as round 2", amounts(retried))
	}
	if got, want := w.wallet(ws), ws.Start-unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertLedger(ws)
}
