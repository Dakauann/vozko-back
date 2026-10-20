package conversation_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	lmw "vozko/domain/lead_message_window"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type continuationEntries struct {
	wce.Repository
	entry *wce.WhatsAppCampaignEntry
}

func (r continuationEntries) FindByID(id string) (*wce.WhatsAppCampaignEntry, error) {
	if r.entry == nil || r.entry.ID != id {
		return nil, wce.ErrEntryNotFound
	}
	return r.entry, nil
}

type continuationLeads struct {
	lead.Repository
	err        error
	optedOutAt *time.Time
	blocked    bool
}

func (r continuationLeads) FindByID(workspaceID, id string) (*lead.Lead, error) {
	if r.err != nil {
		return nil, r.err
	}
	return &lead.Lead{ID: id, WorkspaceID: workspaceID, Number: "5511987654321", Name: "Ana", OptedOutAt: r.optedOutAt, Blocked: r.blocked}, nil
}

type continuationPhones struct {
	businessphone.Repository
}

func (continuationPhones) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return &businessphone.WhatsAppBusinessPhoneNumber{ID: id, OwnerWorkspaceID: "ws-1", DisplayPhoneNumber: "551140001234"}, nil
}

type continuationWindows struct {
	lmw.Repository
	open  bool
	err   error
	asked string
}

func (w *continuationWindows) IsWindowOpen(leadID, businessPhoneID string) (bool, error) {
	w.asked = leadID + "/" + businessPhoneID
	return w.open, w.err
}

type continuationTranscript struct {
	conversation.MessageRepository
	rows []*conversation.Message
}

func (t continuationTranscript) ListByEntry(string, shared.EntryType) ([]*conversation.Message, error) {
	return t.rows, nil
}

type whatsAppContinuationHarness struct {
	uc         *handleWhatsAppMessageUseCase
	ai         *scriptedAI
	client     *fallbackPolicyWhatsAppClient
	history    *capturedHistory
	windows    *continuationWindows
	attendance *recordingAttendance
}

func newWhatsAppContinuationHarness() whatsAppContinuationHarness {
	campaign := &wc.Campaign{ID: "camp-1", WorkspaceID: "ws-1", BusinessPhoneID: "phone-1", AgentID: "agent-7", EnableAgentResponses: true, Name: "Matrículas"}
	h := whatsAppContinuationHarness{
		ai:         &scriptedAI{replies: []string{"Sua matrícula foi concluída!"}},
		client:     &fallbackPolicyWhatsAppClient{},
		history:    &capturedHistory{},
		windows:    &continuationWindows{open: true},
		attendance: &recordingAttendance{},
	}
	h.uc = &handleWhatsAppMessageUseCase{
		aiService:             h.ai,
		wcEntryRepo:           continuationEntries{entry: &wce.WhatsAppCampaignEntry{ID: "entry-1", CampaignID: "camp-1", LeadID: "lead-1", ReceivedBusinessPhoneID: "phone-2"}},
		wcCampaignRepo:        routingCampaigns{byID: map[string]*wc.Campaign{campaign.ID: campaign}},
		leadRepo:              continuationLeads{},
		businessPhoneRepo:     continuationPhones{},
		messageWindowRepo:     h.windows,
		delegations:           stubDelegations{},
		agentRepo:             agentsByID{byID: map[string]*agent.Agent{"agent-7": {ID: "agent-7", WorkspaceID: "ws-1", Name: "Bia", MessagingPrompt: "Você é a Bia."}}},
		whatsappClientFactory: stubAudioClientFactory{client: h.client},
		historyManager:        h.history,
		messageRepo: continuationTranscript{rows: []*conversation.Message{
			{MessageType: conversation.MessageTypeUserMessage, SentBy: conversation.SentByContact("5511987654321"), From: "5511987654321", To: "551140001234", Text: "meu CPF é 123"},
			{MessageType: conversation.MessageTypeAIResponse, SentBy: conversation.SentByAI("agent-7"), From: "551140001234", To: "5511987654321", Text: "Vou verificar."},
		}},
	}
	h.uc.SetAIAttendance(h.attendance)
	return h
}

func officialTurn() AgentContinuationTurn {
	return AgentContinuationTurn{
		WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp, AgentID: "agent-7",
		Note: `O fluxo "Matrícula" que você executou retornou agora.`,
	}
}

func TestAnOfficialWhatsAppContinuationRepliesThroughTheSameReplyCode(t *testing.T) {
	h := newWhatsAppContinuationHarness()

	if err := h.uc.ContinueAgentTurn(context.Background(), officialTurn()); err != nil {
		t.Fatal(err)
	}

	if len(h.client.sent) != 1 || h.client.sent[0].To != "5511987654321" || h.client.sent[0].Body != "Sua matrícula foi concluída!" {
		t.Fatalf("sent = %+v", h.client.sent)
	}
	in := h.ai.inputs[0]
	note := in.Messages[len(in.Messages)-1]
	if note.Role != ai.RoleUser || !strings.Contains(note.Content, "retornou agora") {
		t.Fatalf("note = %+v", note)
	}
	if prev := in.Messages[len(in.Messages)-2]; prev.Role != ai.RoleAssistant || prev.Content != "Vou verificar." {
		t.Fatalf("a continuation adds no customer message, got %+v", prev)
	}
	if len(h.history.records) != 1 || h.history.records[0].SentBy != conversation.SentByAI("agent-7") || h.history.records[0].EntryID != "entry-1" {
		t.Fatalf("records = %+v", h.history.records)
	}
	if len(h.attendance.replies) != 1 || h.attendance.replies[0].AgentID != "agent-7" {
		t.Fatalf("attendance = %+v", h.attendance.replies)
	}
}

func TestTheOfficialWindowIsTheOneTheCustomerLastWroteTo(t *testing.T) {
	h := newWhatsAppContinuationHarness()

	open, err := h.uc.ContinuationWindowOpen(context.Background(), officialTurn())
	if err != nil || !open || h.windows.asked != "lead-1/phone-2" {
		t.Fatalf("open = %v, err = %v, asked = %q", open, err, h.windows.asked)
	}

	h.windows.open = false
	if open, _ := h.uc.ContinuationWindowOpen(context.Background(), officialTurn()); open {
		t.Fatal("a closed 24 h window must refuse")
	}
	h.windows.err = errors.New("db down")
	if _, err := h.uc.ContinuationWindowOpen(context.Background(), officialTurn()); err == nil {
		t.Fatal("an unreadable window must be reported")
	}
}

func TestAnOfficialContinuationRefusesWhenTheCampaignAgentChanged(t *testing.T) {
	h := newWhatsAppContinuationHarness()
	turn := officialTurn()
	turn.AgentID = "agent-old"

	if err := h.uc.ContinueAgentTurn(context.Background(), turn); err == nil {
		t.Fatal("another agent must not speak for the one that started the run")
	}
	if len(h.client.sent) != 0 || len(h.ai.inputs) != 0 {
		t.Fatalf("sent = %d, generated = %d", len(h.client.sent), len(h.ai.inputs))
	}
}

func TestAnOfficialContinuationNeverWritesToALeadWhoOptedOutOrIsBlocked(t *testing.T) {
	optedOut := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		leads  continuationLeads
		reason string
	}{
		"opted out": {continuationLeads{optedOutAt: &optedOut}, "lead_opted_out"},
		"blocked":   {continuationLeads{blocked: true}, "lead_blocked"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newWhatsAppContinuationHarness()
			h.uc.leadRepo = tc.leads

			err := h.uc.ContinueAgentTurn(context.Background(), officialTurn())

			if reason, skipped := replySkipReason(err); !skipped || reason != tc.reason {
				t.Fatalf("err = %v, want the %s skip", err, tc.reason)
			}
			if len(h.client.sent) != 0 || len(h.ai.inputs) != 0 {
				t.Fatalf("sent = %d, generated = %d", len(h.client.sent), len(h.ai.inputs))
			}
		})
	}
}

func TestAnOfficialContinuationTheWorkspaceCannotPayForIsASkipNotASend(t *testing.T) {
	h := newWhatsAppContinuationHarness()
	h.uc.serviceMessageBilling = refusingBilling{}

	err := h.uc.ContinueAgentTurn(context.Background(), officialTurn())

	if reason, skipped := replySkipReason(err); !skipped || reason != replySkipInsufficientBalance {
		t.Fatalf("err = %v, want the insufficient balance skip", err)
	}
	if len(h.client.sent) != 0 || len(h.ai.inputs) != 0 {
		t.Fatalf("sent = %d, generated = %d", len(h.client.sent), len(h.ai.inputs))
	}
}

func TestAnOfficialContinuationWithoutTheLeadSendsNothing(t *testing.T) {
	h := newWhatsAppContinuationHarness()
	h.uc.leadRepo = continuationLeads{err: errors.New("db down")}

	err := h.uc.ContinueAgentTurn(context.Background(), officialTurn())
	if err == nil || len(h.client.sent) != 0 {
		t.Fatalf("err = %v, sent = %d", err, len(h.client.sent))
	}
	if !errors.Is(err, conversation.ErrAgentTurnNotStarted) {
		t.Fatalf("err = %v; a failed read before the model ran may be retried", err)
	}
}

func TestAnOfficialContinuationWhoseAgentChangedIsNotRetried(t *testing.T) {
	h := newWhatsAppContinuationHarness()
	turn := officialTurn()
	turn.AgentID = "agent-old"

	if err := h.uc.ContinueAgentTurn(context.Background(), turn); conversation.TurnRetryable(err) {
		t.Fatalf("err = %v; a changed automation is final", err)
	}
}
