package conversation_usecase

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/shared"
	toolsdomain "vozko/domain/tools"
	businessphone "vozko/domain/whatsapp/business_phone"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/infra/whisper"
)

type downloadingClient struct {
	fallbackPolicyWhatsAppClient
	media []byte
}

func (c *downloadingClient) DownloadMedia(context.Context, string) ([]byte, string, error) {
	return c.media, "", nil
}

type storedMediaStub struct{}

func (storedMediaStub) Store(in conversation.StoreMediaInput) (*conversation.ConversationMedia, error) {
	return &conversation.ConversationMedia{ID: in.ID, URL: "https://cdn.test/" + in.ID}, nil
}

type fixedExtractor struct{ text string }

func (fixedExtractor) CanExtract(string) bool { return true }

func (e fixedExtractor) ExtractText([]byte, string) (*media.TextExtractionResult, error) {
	return &media.TextExtractionResult{Text: e.text, Source: "pdf"}, nil
}

type liveTranscript struct {
	conversation.MessageRepository
	mu    sync.Mutex
	rows  []*conversation.Message
	reads int
}

func (t *liveTranscript) Create(m *conversation.Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rows = append(t.rows, m)
	return nil
}

func (t *liveTranscript) ListByEntry(string, shared.EntryType) ([]*conversation.Message, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reads++
	return append([]*conversation.Message(nil), t.rows...), nil
}

func (t *liveTranscript) GetByID(string) (*conversation.Message, error) {
	return nil, conversation.ErrMessageNotFound
}

func (t *liveTranscript) GetByWhatsAppMessageID(string) (*conversation.Message, error) {
	return nil, conversation.ErrMessageNotFound
}

func (t *liveTranscript) GetByEntryAndExternalMessageID(shared.EntryType, string, string) (*conversation.Message, error) {
	return nil, conversation.ErrMessageNotFound
}

func (t *liveTranscript) add(m *conversation.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rows = append(t.rows, m)
}

type toolCallingAI struct {
	ai.Service
	reply  string
	calls  []ai.ToolCall
	inputs []ai.GenerateInput
}

func (s *toolCallingAI) Generate(_ context.Context, in ai.GenerateInput) (*ai.GenerateOutput, error) {
	s.inputs = append(s.inputs, in)
	return &ai.GenerateOutput{Message: ai.Message{Role: ai.RoleAssistant, Content: s.reply}, ToolCalls: s.calls}, nil
}

type mediaReplyHarness struct {
	uc         *handleWhatsAppMessageUseCase
	ai         *toolCallingAI
	client     *downloadingClient
	history    *capturedHistory
	transcript *liveTranscript
	attendance *recordingAttendance
}

func newMediaReplyHarness(t *testing.T) mediaReplyHarness {
	t.Helper()
	campaign := &wc.Campaign{ID: "camp-1", WorkspaceID: "ws-1", BusinessPhoneID: "phone-1", AgentID: "agent-1", EnableAgentResponses: true}
	entries := &oneConsumerEntries{routingEntries{existing: &wce.WhatsAppCampaignEntry{ID: "entry-1", CampaignID: campaign.ID, LeadID: "lead-ws-1"}}}
	h := mediaReplyHarness{
		ai:         &toolCallingAI{reply: "Recebi seu boleto."},
		client:     &downloadingClient{media: []byte("OggS-audio-bytes")},
		history:    &capturedHistory{},
		transcript: &liveTranscript{},
		attendance: &recordingAttendance{},
	}
	whisperServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"quero saber o preço do curso de enfermagem"}`))
	}))
	t.Cleanup(whisperServer.Close)
	pool, err := whisper.NewPool(whisper.PoolConfig{BaseURLs: []string{whisperServer.URL}})
	if err != nil {
		t.Fatal(err)
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
		messageRepo:           h.transcript,
		mediaStore:            storedMediaStub{},
		textExtractor:         fixedExtractor{text: "Boleto de R$ 120"},
		whisperPool:           pool,
		exitStep:              &capturingExitStep{},
		statusEvents:          &fakeStatusRecorder{},
	}
	h.uc.SetTriggerEvaluator(&waitingRunEvaluator{})
	h.uc.SetAIAttendance(h.attendance)
	return h
}

func customerPayload(message conversation.WhatsAppMessage) *conversation.WhatsAppWebhookPayload {
	message.From, message.Timestamp = "5511987654321", "1792152000"
	return &conversation.WhatsAppWebhookPayload{Entry: []conversation.WhatsAppEntry{{Changes: []conversation.WhatsAppChange{{Value: conversation.WhatsAppChangeValue{
		Metadata: conversation.WhatsAppMetadata{PhoneNumberID: "meta-1", DisplayPhoneNumber: "551140001234"},
		Messages: []conversation.WhatsAppMessage{message},
	}}}}}}
}

func customerDocument() *conversation.WhatsAppWebhookPayload {
	return customerPayload(conversation.WhatsAppMessage{ID: "wamid.doc", Type: "document",
		Document: &conversation.WhatsAppMediaMessage{ID: "media-1", MimeType: "application/pdf", FileName: "boleto.pdf"}})
}

func customerVoiceNote() *conversation.WhatsAppWebhookPayload {
	return customerPayload(conversation.WhatsAppMessage{ID: "wamid.audio", Type: "audio",
		Audio: &conversation.WhatsAppAudioMessage{ID: "audio-1", MimeType: "audio/ogg", Voice: true}})
}

func agentReplies(records []conversation.MessageHistoryRecord) []conversation.MessageHistoryRecord {
	var out []conversation.MessageHistoryRecord
	for _, r := range records {
		if r.MessageType == conversation.MessageTypeAIResponse {
			out = append(out, r)
		}
	}
	return out
}

func assertRecordedAgentReply(t *testing.T, records []conversation.MessageHistoryRecord, body string) {
	t.Helper()
	replies := agentReplies(records)
	if len(replies) != 1 {
		t.Fatalf("agent replies recorded = %+v, want one", replies)
	}
	r := replies[0]
	if r.Text != body || r.SentBy != conversation.SentByAI("agent-1") || r.EntryID != "entry-1" || r.EntryType != shared.EntryTypeWhatsApp ||
		r.From != "551140001234" || r.To != "5511987654321" || r.MessageID != "fallback-msg" || r.Channel != conversation.MessageChannelWhatsApp {
		t.Fatalf("recorded reply = %+v", r)
	}
}

func TestAMediaReplyIsSentRecordedAndCountedLikeAText(t *testing.T) {
	h := newMediaReplyHarness(t)

	if err := h.uc.Execute(context.Background(), customerDocument()); err != nil {
		t.Fatal(err)
	}

	if len(h.client.sent) != 1 || h.client.sent[0].To != "5511987654321" || h.client.sent[0].Body != "Recebi seu boleto." {
		t.Fatalf("sent = %+v", h.client.sent)
	}
	assertRecordedAgentReply(t, h.history.records, "Recebi seu boleto.")
	in := h.ai.inputs[0]
	if last := in.Messages[len(in.Messages)-2]; last.Role != ai.RoleUser || last.Content != conversation.PromptContent("[Document] boleto.pdf", "Boleto de R$ 120") {
		t.Fatalf("the turn must end with the document and its text, got %+v", last)
	}
	assertCountedAsAIReply(t, h.attendance)
}

func assertCountedAsAIReply(t *testing.T, attendance *recordingAttendance) {
	t.Helper()
	if len(attendance.replies) != 1 || attendance.replies[0].AgentID != "agent-1" || attendance.replies[0].EntryID != "entry-1" || attendance.ids[0] != "fallback-msg" {
		t.Fatalf("attendance = %+v %v, want the reply counted once like a text reply", attendance.replies, attendance.ids)
	}
}

func TestAVoiceNoteReplyIsSentRecordedAndCountedLikeAText(t *testing.T) {
	h := newMediaReplyHarness(t)
	h.ai.reply = "O curso custa R$ 300."

	if err := h.uc.Execute(context.Background(), customerVoiceNote()); err != nil {
		t.Fatal(err)
	}

	if len(h.client.sent) != 1 || h.client.sent[0].To != "5511987654321" || h.client.sent[0].Body != "O curso custa R$ 300." {
		t.Fatalf("sent = %+v", h.client.sent)
	}
	assertRecordedAgentReply(t, h.history.records, "O curso custa R$ 300.")
	in := h.ai.inputs[0]
	if last := in.Messages[len(in.Messages)-2]; last.Role != ai.RoleUser || last.Content != conversation.PromptContent(conversation.SpokenText("quero saber o preço do curso de enfermagem"), "") {
		t.Fatalf("the turn must end with the transcript, got %+v", last)
	}
	assertCountedAsAIReply(t, h.attendance)
}

type officialInbound struct {
	name    string
	payload func() *conversation.WhatsAppWebhookPayload
}

func officialInbounds() []officialInbound {
	return []officialInbound{
		{"text", func() *conversation.WhatsAppWebhookPayload { return customerText("12345678900") }},
		{"media", customerDocument},
		{"audio", customerVoiceNote},
	}
}

func historyOfType(records []conversation.MessageHistoryRecord, kind conversation.MessageType) []conversation.MessageHistoryRecord {
	var out []conversation.MessageHistoryRecord
	for _, r := range records {
		if r.MessageType == kind {
			out = append(out, r)
		}
	}
	return out
}

func TestEveryOfficialReplyPathRecordsItsToolCallsWithoutSecrets(t *testing.T) {
	for _, tc := range officialInbounds() {
		t.Run(tc.name, func(t *testing.T) {
			h := newMediaReplyHarness(t)
			h.ai.calls = []ai.ToolCall{{
				Name:      "consultar_matricula",
				Arguments: map[string]interface{}{"cpf": "529.982.247-25", "api_key": "sk-live-segredo", "curso": "enfermagem"},
				Result:    &toolsdomain.ExecutionResult{Result: map[string]interface{}{"access_token": "tok-segredo", "status": "ativa"}},
			}}

			if err := h.uc.Execute(context.Background(), tc.payload()); err != nil {
				t.Fatal(err)
			}

			calls := historyOfType(h.history.records, conversation.MessageTypeToolCall)
			results := historyOfType(h.history.records, conversation.MessageTypeToolResult)
			if len(calls) != 1 || len(results) != 1 {
				t.Fatalf("tool call rows = %d, tool result rows = %d, want one each", len(calls), len(results))
			}
			if !strings.HasPrefix(calls[0].Text, "[Tool Call] consultar_matricula: ") || !strings.Contains(calls[0].Text, "enfermagem") {
				t.Fatalf("tool call row = %q", calls[0].Text)
			}
			for _, row := range []string{calls[0].Text, results[0].Text} {
				for _, secret := range []string{"sk-live-segredo", "tok-segredo", "529.982.247"} {
					if strings.Contains(row, secret) {
						t.Fatalf("history row %q carries %q", row, secret)
					}
				}
			}
			if !strings.Contains(results[0].Text, "ativa") {
				t.Fatalf("tool result row = %q", results[0].Text)
			}
		})
	}
}

func TestToolHistoryRowsCarryTheToolCallIDOfTheRecord(t *testing.T) {
	h := newMediaReplyHarness(t)
	h.ai.calls = []ai.ToolCall{
		{ID: "call_model_7", Name: "consultar_matricula", Arguments: map[string]interface{}{}, Result: &toolsdomain.ExecutionResult{Result: "ok"}},
		{Name: "sem_id", Arguments: map[string]interface{}{}, Result: &toolsdomain.ExecutionResult{Result: "ok"}},
	}
	if err := h.uc.Execute(context.Background(), customerText("12345678900")); err != nil {
		t.Fatal(err)
	}
	calls := historyOfType(h.history.records, conversation.MessageTypeToolCall)
	results := historyOfType(h.history.records, conversation.MessageTypeToolResult)
	if len(calls) != 2 || len(results) != 2 {
		t.Fatalf("tool call rows = %d, tool result rows = %d", len(calls), len(results))
	}
	for _, row := range []conversation.MessageHistoryRecord{calls[0], results[0]} {
		var meta map[string]interface{}
		if err := json.Unmarshal(row.Metadata, &meta); err != nil || meta[conversation.ToolCallIDMetadataKey] != "call_model_7" {
			t.Fatalf("row %q metadata = %s, want the tool call id", row.Text, row.Metadata)
		}
	}
	for _, row := range []conversation.MessageHistoryRecord{calls[1], results[1]} {
		if len(row.Metadata) != 0 {
			t.Fatalf("a call without an id keeps the old row shape, got %s", row.Metadata)
		}
	}
}

func TestAnOfficialTurnThatWaitedForAContinuationSeesItsReply(t *testing.T) {
	for _, tc := range officialInbounds() {
		t.Run(tc.name, func(t *testing.T) {
			h := newMediaReplyHarness(t)
			lock := NewAgentTurnLock(newLockState())
			lock.poll = time.Millisecond
			h.uc.SetAgentTurnLock(lock)
			continuation, err := lock.Acquire(context.Background(), "entry-1", shared.EntryTypeWhatsApp)
			if err != nil {
				t.Fatal(err)
			}

			done := make(chan error, 1)
			go func() { done <- h.uc.Execute(context.Background(), tc.payload()) }()
			eventually(t, "the inbound turn to read the conversation before waiting", func() bool {
				h.transcript.mu.Lock()
				defer h.transcript.mu.Unlock()
				return h.transcript.reads > 0
			})
			time.Sleep(20 * time.Millisecond)
			h.transcript.add(&conversation.Message{
				ID: "continuation-reply", EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp, MessageType: conversation.MessageTypeAIResponse,
				SentBy: conversation.SentByAI("agent-1"), From: "551140001234", To: "5511987654321", Text: "Sua matrícula foi concluída!",
			})
			continuation()
			if err := <-done; err != nil {
				t.Fatal(err)
			}

			if len(h.ai.inputs) != 1 {
				t.Fatalf("generated %d turns", len(h.ai.inputs))
			}
			for _, m := range h.ai.inputs[0].Messages {
				if m.Role == ai.RoleAssistant && m.Content == "Sua matrícula foi concluída!" {
					return
				}
			}
			t.Fatalf("the turn did not see the continuation's reply: %+v", h.ai.inputs[0].Messages)
		})
	}
}
