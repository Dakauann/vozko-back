package conversation_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/agent"
	"vozko/domain/conversation"
	businessphone "vozko/domain/whatsapp/business_phone"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type oneConsumerEntries struct {
	routingEntries
}

func (*oneConsumerEntries) UpdateReceivedBusinessPhone(string, string) error { return nil }

type oneConsumerWhatsApp struct {
	uc        *handleWhatsAppMessageUseCase
	ai        *scriptedAI
	client    *fallbackPolicyWhatsAppClient
	history   *capturedHistory
	evaluator *waitingRunEvaluator
}

func agentHeldWhatsApp(evaluator *waitingRunEvaluator) oneConsumerWhatsApp {
	campaign := &wc.Campaign{ID: "camp-1", WorkspaceID: "ws-1", BusinessPhoneID: "phone-1", AgentID: "agent-1", EnableAgentResponses: true}
	entries := &oneConsumerEntries{routingEntries{existing: &wce.WhatsAppCampaignEntry{ID: "entry-1", CampaignID: campaign.ID, LeadID: "lead-ws-1"}}}
	h := oneConsumerWhatsApp{
		ai:        &scriptedAI{replies: []string{"Oi! Como posso ajudar?"}},
		client:    &fallbackPolicyWhatsAppClient{},
		history:   &capturedHistory{},
		evaluator: evaluator,
	}
	h.uc = &handleWhatsAppMessageUseCase{
		aiService:             h.ai,
		businessPhoneRepo:     routingPhones{phone: &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-1", OwnerWorkspaceID: "ws-1", DisplayPhoneNumber: "551140001234"}},
		wcEntryRepo:           entries,
		wcCampaignRepo:        routingCampaigns{byID: map[string]*wc.Campaign{campaign.ID: campaign}},
		leadRepo:              routingLeads{},
		delegations:           stubDelegations{},
		agentRepo:             agentsByID{byID: map[string]*agent.Agent{"agent-1": {ID: "agent-1", WorkspaceID: "ws-1", Name: "Bia", MessagingPrompt: "Você é a Bia."}}},
		whatsappClientFactory: stubAudioClientFactory{client: h.client},
		historyManager:        h.history,
		exitStep:              &capturingExitStep{},
		statusEvents:          &fakeStatusRecorder{},
	}
	h.uc.SetTriggerEvaluator(evaluator)
	return h
}

func customerText(body string) *conversation.WhatsAppWebhookPayload {
	return &conversation.WhatsAppWebhookPayload{Entry: []conversation.WhatsAppEntry{{Changes: []conversation.WhatsAppChange{{Value: conversation.WhatsAppChangeValue{
		Metadata: conversation.WhatsAppMetadata{PhoneNumberID: "meta-1", DisplayPhoneNumber: "551140001234"},
		Messages: []conversation.WhatsAppMessage{{From: "5511987654321", ID: "wamid.in", Timestamp: "1792152000", Type: "text", Text: &conversation.WhatsAppTextMessage{Body: "12345678900"}}},
	}}}}}}
}

func eventually(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestOnOfficialWhatsAppAWaitingRunGetsTheReplyAndTheAgentIsNotCalled(t *testing.T) {
	h := agentHeldWhatsApp(&waitingRunEvaluator{waiting: waitingRun()})

	if err := h.uc.Execute(context.Background(), customerText("12345678900")); err != nil {
		t.Fatal(err)
	}

	eventually(t, "the waiting run to wake", func() bool { woken, _ := h.evaluator.counts(); return woken == 1 })
	if len(h.ai.inputs) != 0 || len(h.client.sent) != 0 {
		t.Fatalf("generated %d, sent %d; the agent answered a message the waiting run consumed", len(h.ai.inputs), len(h.client.sent))
	}
	if len(h.history.records) != 1 || h.history.records[0].Text != "12345678900" {
		t.Fatalf("records = %+v, the message must still be recorded", h.history.records)
	}
}

func TestOnOfficialWhatsAppWithoutAWaitingRunTheAgentReplies(t *testing.T) {
	h := agentHeldWhatsApp(&waitingRunEvaluator{})

	if err := h.uc.Execute(context.Background(), customerText("12345678900")); err != nil {
		t.Fatal(err)
	}

	if len(h.ai.inputs) != 1 || len(h.client.sent) != 1 || h.client.sent[0].Body != "Oi! Como posso ajudar?" {
		t.Fatalf("generated %d, sent %+v", len(h.ai.inputs), h.client.sent)
	}
	if woken, _ := h.evaluator.counts(); woken != 0 {
		t.Fatalf("woke %d runs with none waiting", woken)
	}
}

func TestOnOfficialWhatsAppAnUnreadableWaitingRunMeansNoAutomatedReply(t *testing.T) {
	h := agentHeldWhatsApp(&waitingRunEvaluator{lookupErr: errors.New("db down")})

	if err := h.uc.Execute(context.Background(), customerText("12345678900")); err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)
	woken, evaluated := h.evaluator.counts()
	if len(h.ai.inputs) != 0 || len(h.client.sent) != 0 || woken != 0 || evaluated != 0 {
		t.Fatalf("generated %d, sent %d, woken %d, evaluated %d; a failed lookup must silence every automated reply",
			len(h.ai.inputs), len(h.client.sent), woken, evaluated)
	}
	if len(h.history.records) != 1 {
		t.Fatalf("records = %d, the message must still be recorded", len(h.history.records))
	}
}
