package workflow_usecase

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	lead_domain "vozko/domain/lead"
	lead_message_window_domain "vozko/domain/lead_message_window"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

var (
	_ conversation.WhatsAppClient            = (*simWhatsAppClient)(nil)
	_ conversation.WhatsAppClientFactory     = (*simWhatsAppClientFactory)(nil)
	_ conversation.MessageHistoryManager     = (*simHistoryManager)(nil)
	_ lead_domain.Repository                 = (*simLeadRepo)(nil)
	_ wce.Repository                         = (*simWhatsAppEntryRepo)(nil)
	_ businessphone.Repository               = (*simBusinessPhoneRepo)(nil)
	_ lead_message_window_domain.Repository  = (*simMessageWindowRepo)(nil)
	_ conversation.MessageRepository         = (*simMessageRepo)(nil)
	_ balance.ConsumeWhatsappTemplateUseCase = (*simBalanceConsumer)(nil)
	_ node_executors.SubWorkflowRunner       = (*simSubWorkflowRunner)(nil)
	_ workflow.WorkflowRunRepository         = (*simRunRepo)(nil)
	_ workflow.WorkflowRunLogRepository      = (*simLogRepo)(nil)
)

type SimOutboundMessage struct {
	Type        string      `json:"type"`
	Text        string      `json:"text"`
	To          string      `json:"to"`
	MessageID   string      `json:"messageId"`
	Extra       interface{} `json:"extra,omitempty"`
	AudioBase64 string      `json:"audioBase64,omitempty"`
	AudioMime   string      `json:"audioMime,omitempty"`
}

type simWhatsAppClient struct {
	outCh chan<- SimOutboundMessage
}

func newSimWhatsAppClient(outCh chan<- SimOutboundMessage) *simWhatsAppClient {
	return &simWhatsAppClient{outCh: outCh}
}

func (c *simWhatsAppClient) fakeOutput(text, msgType string) *conversation.SendTextMessageOutput {
	id := "sim-" + uuid.New().String()
	c.outCh <- SimOutboundMessage{Type: msgType, Text: text, MessageID: id, To: "5511999990000"}
	return &conversation.SendTextMessageOutput{MessageID: id}
}

func (c *simWhatsAppClient) SendTextMessage(_ context.Context, input conversation.SendTextMessageInput) (*conversation.SendTextMessageOutput, error) {
	return c.fakeOutput(input.Body, "text"), nil
}

func (c *simWhatsAppClient) SendButtonMessage(_ context.Context, input conversation.SendButtonMessageInput) (*conversation.SendTextMessageOutput, error) {
	labelsJSON, _ := json.Marshal(input.Buttons)
	return c.fakeOutput(input.BodyText+" [buttons:"+string(labelsJSON)+"]", "button"), nil
}

func (c *simWhatsAppClient) SendListMessage(_ context.Context, input conversation.SendListMessageInput) (*conversation.SendTextMessageOutput, error) {
	sectionsJSON, _ := json.Marshal(input.Sections)
	return c.fakeOutput(input.BodyText+" [list:"+string(sectionsJSON)+"]", "list"), nil
}

func (c *simWhatsAppClient) SendCallPermissionRequest(_ context.Context, input conversation.SendCallPermissionRequestInput) (*conversation.SendTextMessageOutput, error) {
	return c.fakeOutput(input.BodyText+" [call_permission_request]", "call_permission_request"), nil
}

func (c *simWhatsAppClient) SendImageMessage(_ context.Context, input conversation.SendImageMessageInput) (*conversation.SendTextMessageOutput, error) {
	return c.fakeOutput("[image] "+input.Caption, "media"), nil
}

func (c *simWhatsAppClient) SendVideoMessage(_ context.Context, input conversation.SendVideoMessageInput) (*conversation.SendTextMessageOutput, error) {
	return c.fakeOutput("[video] "+input.Caption, "media"), nil
}

func (c *simWhatsAppClient) SendDocumentMessage(_ context.Context, input conversation.SendDocumentMessageInput) (*conversation.SendTextMessageOutput, error) {
	return c.fakeOutput("[document] "+input.Caption, "media"), nil
}

func (c *simWhatsAppClient) SendStickerMessage(_ context.Context, _ conversation.SendStickerMessageInput) (*conversation.SendTextMessageOutput, error) {
	return c.fakeOutput("[sticker]", "media"), nil
}

func (c *simWhatsAppClient) SendAudioMessage(_ context.Context, input conversation.SendAudioMessageInput) (*conversation.SendTextMessageOutput, error) {
	return c.fakeOutput("[audio] "+input.AudioURL, "media"), nil
}

func (c *simWhatsAppClient) SendAudioBytes(_ context.Context, _ string, data []byte, filename string, mime string) (*conversation.SendTextMessageOutput, error) {

	id := "sim-" + uuid.New().String()
	if strings.TrimSpace(mime) == "" {
		mime = "audio/ogg"
	}
	caption := "[audio]"
	if filename != "" {
		caption = "[audio] " + filename
	}
	encoded := ""
	if len(data) > 0 {
		encoded = base64.StdEncoding.EncodeToString(data)
	}
	c.outCh <- SimOutboundMessage{
		Type:        "audio",
		Text:        caption,
		MessageID:   id,
		To:          "5511999990000",
		AudioBase64: encoded,
		AudioMime:   mime,
	}
	return &conversation.SendTextMessageOutput{MessageID: id}, nil
}

func (c *simWhatsAppClient) SendTemplateMessage(_ context.Context, input conversation.SendTemplateMessageInput) (*conversation.SendTextMessageOutput, error) {
	return c.fakeOutput("[template:"+input.TemplateName+"]", "template"), nil
}

func (c *simWhatsAppClient) SendTypingIndicator(_ context.Context, _ string) error { return nil }
func (c *simWhatsAppClient) MarkMessageAsRead(_ context.Context, _ string) error   { return nil }

func (c *simWhatsAppClient) UploadAudio(_ context.Context, _ []byte, _ string) (string, error) {
	return "sim-media-" + uuid.New().String(), nil
}
func (c *simWhatsAppClient) UploadImage(_ context.Context, _ []byte, _ string, _ string) (string, error) {
	return "sim-media-" + uuid.New().String(), nil
}
func (c *simWhatsAppClient) UploadMedia(_ context.Context, _ []byte, _ string, _ string) (string, error) {
	return "sim-media-" + uuid.New().String(), nil
}
func (c *simWhatsAppClient) DownloadMedia(_ context.Context, _ string) ([]byte, string, error) {
	return []byte{}, "application/octet-stream", nil
}

func (c *simWhatsAppClient) ListTemplates(_ context.Context, _ conversation.ListTemplatesInput) (*conversation.ListTemplatesOutput, error) {
	return &conversation.ListTemplatesOutput{}, nil
}
func (c *simWhatsAppClient) GetTemplate(_ context.Context, _ string) (*conversation.Template, error) {
	return nil, nil
}
func (c *simWhatsAppClient) CreateTemplate(_ context.Context, _ conversation.CreateTemplateInput) (*conversation.CreateTemplateOutput, error) {
	return &conversation.CreateTemplateOutput{}, nil
}
func (c *simWhatsAppClient) UpdateTemplate(_ context.Context, _ string, _ conversation.UpdateTemplateInput) error {
	return nil
}
func (c *simWhatsAppClient) DeleteTemplate(_ context.Context, _ conversation.DeleteTemplateInput) error {
	return nil
}
func (c *simWhatsAppClient) UploadMediaForTemplate(_ context.Context, _ conversation.UploadMediaForTemplateInput) (string, error) {
	return "sim-media-" + uuid.New().String(), nil
}

type simWhatsAppClientFactory struct {
	client *simWhatsAppClient
}

func newSimWhatsAppClientFactory(outCh chan<- SimOutboundMessage) *simWhatsAppClientFactory {
	return &simWhatsAppClientFactory{client: newSimWhatsAppClient(outCh)}
}

func (f *simWhatsAppClientFactory) ClientForPhone(_ string) (conversation.WhatsAppClient, error) {
	return f.client, nil
}
func (f *simWhatsAppClientFactory) ClientForWABA(_ string) (conversation.WhatsAppClient, error) {
	return f.client, nil
}
func (f *simWhatsAppClientFactory) WABAIdForPhone(_ string) (string, error) {
	return "", nil
}

type simLeadRepo struct{}

func (r *simLeadRepo) FindByID(_ string, id string) (*lead_domain.Lead, error) {
	return &lead_domain.Lead{
		ID:        id,
		Number:    "5511999990000",
		Name:      "Contato Simulado",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}, nil
}
func (r *simLeadRepo) Create(_ *lead_domain.Lead) error { return nil }
func (r *simLeadRepo) FindByIDs(_ string, _ []string) ([]*lead_domain.Lead, error) {
	return nil, nil
}
func (r *simLeadRepo) FindByNumber(_ string, _ string) (*lead_domain.Lead, error) {
	return nil, nil
}
func (r *simLeadRepo) FindOrCreate(_ string, _ string, _ lead_domain.LeadUpdate) (*lead_domain.Lead, bool, error) {
	return nil, false, nil
}
func (r *simLeadRepo) FindOrCreateMany(_ string, _ []lead_domain.BulkLeadInput) (map[string]*lead_domain.Lead, error) {
	return nil, nil
}
func (r *simLeadRepo) Update(_ string, _ string, _ lead_domain.LeadUpdate) error { return nil }
func (r *simLeadRepo) Delete(_ string, _ string) error                           { return nil }
func (r *simLeadRepo) List(_ lead_domain.ListLeadsInput) (*shared.PaginatedResult[*lead_domain.Lead], error) {
	return &shared.PaginatedResult[*lead_domain.Lead]{}, nil
}
func (r *simLeadRepo) ListWithSummary(_ lead_domain.ListLeadsInput) (*shared.PaginatedResult[*lead_domain.LeadWithSummary], error) {
	return &shared.PaginatedResult[*lead_domain.LeadWithSummary]{}, nil
}
func (r *simLeadRepo) ResolveCampaignNames(_ []string) map[string]string { return nil }

type simWhatsAppEntryRepo struct {
	leadID string
}

func (r *simWhatsAppEntryRepo) FindByID(id string) (*wce.WhatsAppCampaignEntry, error) {
	return &wce.WhatsAppCampaignEntry{
		ID:                      id,
		CampaignID:              "sim-campaign",
		LeadID:                  r.leadID,
		Status:                  wce.SendStatusSent,
		ReceivedBusinessPhoneID: "sim-business-phone",
		CreatedAt:               time.Now().UTC(),
		UpdatedAt:               time.Now().UTC(),
	}, nil
}

func (r *simWhatsAppEntryRepo) FindByMessageID(messageID string) (*wce.WhatsAppCampaignEntry, error) {
	return &wce.WhatsAppCampaignEntry{
		ID:                      "sim-entry",
		CampaignID:              "sim-campaign",
		LeadID:                  r.leadID,
		MessageID:               messageID,
		Status:                  wce.SendStatusSent,
		ReceivedBusinessPhoneID: "sim-business-phone",
		CreatedAt:               time.Now().UTC(),
		UpdatedAt:               time.Now().UTC(),
	}, nil
}

func (r *simWhatsAppEntryRepo) GetCampaignForEntry(_ string) (*wce.EntryCampaignInfo, error) {
	return &wce.EntryCampaignInfo{
		CampaignID:      "sim-campaign",
		BusinessPhoneID: "sim-business-phone",
	}, nil
}
func (r *simWhatsAppEntryRepo) Create(_ *wce.WhatsAppCampaignEntry) error { return nil }
func (r *simWhatsAppEntryRepo) CreateMany(_ []wce.WhatsAppCampaignEntry) ([]wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *simWhatsAppEntryRepo) FindByIDs(_ []string) ([]*wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *simWhatsAppEntryRepo) FindByCampaignAndLead(_, _ string) (*wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *simWhatsAppEntryRepo) FindByCampaignAndNumber(_, _ string) (*wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *simWhatsAppEntryRepo) Delete(_ string) error             { return nil }
func (r *simWhatsAppEntryRepo) DeleteByCampaignID(_ string) error { return nil }
func (r *simWhatsAppEntryRepo) List(_ wce.ListEntriesInput) (*shared.PaginatedResult[*wce.WhatsAppCampaignEntry], error) {
	return &shared.PaginatedResult[*wce.WhatsAppCampaignEntry]{}, nil
}
func (r *simWhatsAppEntryRepo) ListByCampaignID(_ string) ([]wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *simWhatsAppEntryRepo) ListByLeadID(_ string) ([]wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *simWhatsAppEntryRepo) ListRecentlyUpdated(_ string, _ int) ([]wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *simWhatsAppEntryRepo) CountByCampaignID(_ string) (int64, error) { return 0, nil }
func (r *simWhatsAppEntryRepo) CountByStatus(_ string) (*wce.StatusCounts, error) {
	return &wce.StatusCounts{}, nil
}
func (r *simWhatsAppEntryRepo) CountByStatusForCampaigns(_ []string) (map[string]*wce.StatusCounts, error) {
	return map[string]*wce.StatusCounts{}, nil
}
func (r *simWhatsAppEntryRepo) UpdateStatus(_ string, _ wce.SendStatus, _ string, _ int, _ string) error {
	return nil
}
func (r *simWhatsAppEntryRepo) UpdateReceivedBusinessPhone(_, _ string) error { return nil }
func (r *simWhatsAppEntryRepo) UpdateStatusByMessageID(_ string, _ wce.SendStatus) error {
	return nil
}
func (r *simWhatsAppEntryRepo) UpdateStatusByNumber(_, _ string, _ wce.SendStatus, _ string) error {
	return nil
}
func (r *simWhatsAppEntryRepo) ResetAllStatuses(_ string) (int64, error) { return 0, nil }
func (r *simWhatsAppEntryRepo) ReplaceCampaignEntries(_ string, _ []wce.WhatsAppCampaignEntry) error {
	return nil
}
func (r *simWhatsAppEntryRepo) UpsertCampaignEntries(_ string, _ []wce.WhatsAppCampaignEntry) error {
	return nil
}
func (r *simWhatsAppEntryRepo) ListByStatus(_ string, _ wce.SendStatus, _ int) ([]wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *simWhatsAppEntryRepo) ListEntriesWithLeads(_ wce.ListEntriesInput) (*shared.PaginatedResult[*wce.EntryWithLead], error) {
	return &shared.PaginatedResult[*wce.EntryWithLead]{}, nil
}
func (r *simWhatsAppEntryRepo) ListEntriesWithLeadsForUser(_ wce.ListEntriesForUserInput) ([]*wce.EntryWithLead, int64, error) {
	return nil, 0, nil
}
func (r *simWhatsAppEntryRepo) CanUserAccessEntry(_, _ string, _ bool) (bool, error) {
	return true, nil
}
func (r *simWhatsAppEntryRepo) GetAccessibleEntryIDs(_ string, _ bool) ([]string, error) {
	return nil, nil
}
func (r *simWhatsAppEntryRepo) GetEntryIDsByCampaign(_ string) ([]string, error) {
	return nil, nil
}
func (r *simWhatsAppEntryRepo) FindByNumber(_ string) (*wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *simWhatsAppEntryRepo) FindByNumberAndBusinessPhone(_, _ string) (*wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *simWhatsAppEntryRepo) UpdateAutomationEnabled(_ string, _ *bool) error { return nil }
func (r *simWhatsAppEntryRepo) UpdateMetadata(_ string, _ map[string]interface{}) error {
	return nil
}
func (r *simWhatsAppEntryRepo) UpdateConversationStatus(_ string, _ wce.ConversationStatusWrite) error {
	return nil
}
func (r *simWhatsAppEntryRepo) ListEligibleForAutoClose(int) ([]wce.AutoCloseCandidate, error) {
	return nil, nil
}
func (r *simWhatsAppEntryRepo) ListEligibleForMaxAge(int) ([]wce.AutoCloseCandidate, error) {
	return nil, nil
}
func (r *simWhatsAppEntryRepo) CountByConversationStatus(_ string) (map[string]int64, error) {
	return nil, nil
}
func (r *simWhatsAppEntryRepo) CountByConversationStatusForWorkspace(_ string) (map[string]int64, error) {
	return nil, nil
}

type simBusinessPhoneRepo struct {
	workspaceID string
}

func (r *simBusinessPhoneRepo) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = "sim-business-phone"
	}
	return &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                 id,
		WABAId:             "sim-waba-id",
		OwnerWorkspaceID:   strings.TrimSpace(r.workspaceID),
		DisplayPhoneNumber: "5511888880000",
		VerifiedName:       "Simulação",
		Status:             businessphone.StatusConnected,
	}, nil
}
func (r *simBusinessPhoneRepo) Create(_ *businessphone.WhatsAppBusinessPhoneNumber) error { return nil }
func (r *simBusinessPhoneRepo) Update(_ string, _ *businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (r *simBusinessPhoneRepo) Delete(_ string) error { return nil }
func (r *simBusinessPhoneRepo) FindByMetaPhoneNumberID(_ string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (r *simBusinessPhoneRepo) FindByDisplayPhoneNumber(_ string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (r *simBusinessPhoneRepo) FindByWABAId(_ string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (r *simBusinessPhoneRepo) List(_ businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	return &shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber]{}, nil
}
func (r *simBusinessPhoneRepo) ListAll() ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (r *simBusinessPhoneRepo) BatchUpdate(_ []*businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (r *simBusinessPhoneRepo) UpdateStatus(_ string, _ businessphone.Status) error { return nil }
func (r *simBusinessPhoneRepo) UpdateCallsEnabled(_ string, _ bool) error           { return nil }
func (r *simBusinessPhoneRepo) UpdateBusinessProfile(_ string, _ businessphone.BusinessProfile) error {
	return nil
}
func (r *simBusinessPhoneRepo) SyncFromMeta(_ *businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (r *simBusinessPhoneRepo) ClearAccessToken(_ string) error { return nil }
func (r *simBusinessPhoneRepo) ClearOwner(_ string) error       { return nil }
func (r *simBusinessPhoneRepo) FindByMetaPhoneNumberIDUnscoped(_ string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, businessphone.ErrPhoneNumberNotFound
}
func (r *simBusinessPhoneRepo) Restore(_ string) error { return nil }

type simMessageWindowRepo struct{}

func (r *simMessageWindowRepo) IsWindowOpen(_, _ string) (bool, error) { return true, nil }
func (r *simMessageWindowRepo) RecordMessage(_, _ string) (*lead_message_window_domain.LeadMessageWindow, error) {
	return &lead_message_window_domain.LeadMessageWindow{}, nil
}
func (r *simMessageWindowRepo) FindByLeadAndBusinessPhone(_, _ string) (*lead_message_window_domain.LeadMessageWindow, error) {
	return nil, nil
}
func (r *simMessageWindowRepo) FindAllByLead(_ string) ([]*lead_message_window_domain.LeadMessageWindow, error) {
	return nil, nil
}
func (r *simMessageWindowRepo) FindOpenWindowsByLeadIDs(_ []string, _ string) (map[string]*lead_message_window_domain.LeadMessageWindow, error) {
	return nil, nil
}
func (r *simMessageWindowRepo) GetLastMessageTime(_, _ string) (*time.Time, error) {
	return nil, nil
}

type simHistoryManager struct {
	mu      sync.Mutex
	records []conversation.MessageHistoryRecord
}

func (m *simHistoryManager) Record(_ context.Context, _ conversation.MessageHistoryDirection, record conversation.MessageHistoryRecord) error {
	m.mu.Lock()
	m.records = append(m.records, record)
	m.mu.Unlock()
	return nil
}

type simBalanceConsumer struct{}

func (b *simBalanceConsumer) Execute(_, _, _ string) (*balance.Transaction, error) {
	return &balance.Transaction{}, nil
}
func (b *simBalanceConsumer) Refund(_, _, _ string) error                      { return nil }
func (b *simBalanceConsumer) GetTemplateCostMicros(_, _ string) (int64, error) { return 0, nil }

type simMessageRepo struct {
	mu       sync.Mutex
	messages []*conversation.Message
}

func (r *simMessageRepo) Create(msg *conversation.Message) error {
	r.mu.Lock()
	r.messages = append(r.messages, msg)
	r.mu.Unlock()
	return nil
}

func (r *simMessageRepo) Update(_ string, _ *conversation.Message) error { return nil }
func (r *simMessageRepo) Delete(_ string) error                          { return nil }
func (r *simMessageRepo) GetByID(_ string) (*conversation.Message, error) {
	return nil, nil
}

func (r *simMessageRepo) ListByEntry(entryID string, entryType shared.EntryType) ([]*conversation.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []*conversation.Message
	for _, m := range r.messages {
		if m.EntryID == entryID && m.EntryType == entryType {
			result = append(result, m)
		}
	}
	return result, nil
}

func (r *simMessageRepo) ListByEntryPaginated(_ conversation.ListMessagesInput) ([]*conversation.Message, error) {
	return nil, nil
}
func (r *simMessageRepo) ListByLeadID(_ string) ([]*conversation.Message, error) {
	return nil, nil
}
func (r *simMessageRepo) MarkAsRead(_ conversation.MarkAsReadInput) (int64, error) { return 0, nil }
func (r *simMessageRepo) CountUnreadByEntry(_ string, _ shared.EntryType) (int64, error) {
	return 0, nil
}
func (r *simMessageRepo) CountUnreadByEntries(_ []string, _ shared.EntryType) ([]conversation.UnreadCount, error) {
	return nil, nil
}
func (r *simMessageRepo) DeleteByEntry(_ string, _ shared.EntryType) error { return nil }
func (r *simMessageRepo) DeleteByCampaignID(_ string, _ shared.EntryType) (int64, error) {
	return 0, nil
}
func (r *simMessageRepo) CountByCampaignID(_ string, _ shared.EntryType) (int64, error) {
	return 0, nil
}
func (r *simMessageRepo) GetEntriesWithMessages(_ string, _ []string, _ shared.EntryType, _, _ int, _ string) ([]conversation.EntryWithLastMessage, int64, error) {
	return nil, 0, nil
}
func (r *simMessageRepo) SearchEntriesWithMessages(_ conversation.SearchEntriesInput) ([]conversation.EntryWithLastMessage, int64, error) {
	return nil, 0, nil
}
func (r *simMessageRepo) SearchEntriesByFilter(_ conversation.SearchByFilterInput) ([]conversation.EntryWithLastMessage, int64, error) {
	return nil, 0, nil
}
func (r *simMessageRepo) SearchMessagesByEntry(_ conversation.SearchMessagesByEntryInput) ([]*conversation.Message, int64, error) {
	return nil, 0, nil
}
func (r *simMessageRepo) GetEntryLastMessage(_ string, _ shared.EntryType) (*conversation.EntryWithLastMessage, error) {
	return nil, nil
}
func (r *simMessageRepo) CountByEntry(_ string, _ shared.EntryType) (int64, error) {
	return 0, nil
}
func (r *simMessageRepo) GetByWhatsAppMessageID(_ string) (*conversation.Message, error) {
	return nil, nil
}
func (r *simMessageRepo) UpdateDeliveryStatus(_ string, _ conversation.DeliveryStatus) error {
	return nil
}
func (r *simMessageRepo) ClearAll() error { return nil }

type simRunRepo struct {
	mu       sync.Mutex
	runs     map[string]*workflow.WorkflowRun
	onUpdate func(run *workflow.WorkflowRun)
}

func newSimRunRepo(onUpdate func(run *workflow.WorkflowRun)) *simRunRepo {
	return &simRunRepo{
		runs:     make(map[string]*workflow.WorkflowRun),
		onUpdate: onUpdate,
	}
}

func (r *simRunRepo) Create(run *workflow.WorkflowRun) error {
	r.mu.Lock()
	r.runs[run.ID] = run
	r.mu.Unlock()
	return nil
}

func (r *simRunRepo) Update(run *workflow.WorkflowRun) error {
	r.mu.Lock()
	r.runs[run.ID] = run
	cb := r.onUpdate
	r.mu.Unlock()
	if cb != nil {
		cb(run)
	}
	return nil
}

func (r *simRunRepo) FindByID(runID string) (*workflow.WorkflowRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[runID]
	if !ok {
		return nil, nil
	}
	return run, nil
}

func (r *simRunRepo) FindActiveByEntry(workflowID, entryID string) (*workflow.WorkflowRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, run := range r.runs {
		if run.WorkflowID == workflowID && run.EntryID == entryID && !run.Status.IsTerminal() {
			return run, nil
		}
	}
	return nil, nil
}

func (r *simRunRepo) FindActiveByEntryAndTrigger(workflowID, entryID, triggerNodeID string) (*workflow.WorkflowRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, run := range r.runs {
		if run.WorkflowID == workflowID && run.EntryID == entryID && run.TriggerNodeID == triggerNodeID && !run.Status.IsTerminal() {
			return run, nil
		}
	}
	return nil, nil
}

func (r *simRunRepo) FindWaitingReplyByEntry(entryID string) (*workflow.WorkflowRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, run := range r.runs {
		if run.EntryID == entryID && run.Status == workflow.RunStatusWaiting && run.WaitReason == workflow.WaitReasonReply {
			return run, nil
		}
	}
	return nil, nil
}

func (r *simRunRepo) List(_ workflow.ListRunsInput) (*shared.PaginatedResult[*workflow.WorkflowRun], error) {
	return &shared.PaginatedResult[*workflow.WorkflowRun]{}, nil
}
func (r *simRunRepo) FindWakeableRuns(_ int64, _ int) ([]*workflow.WorkflowRun, error) {
	return nil, nil
}
func (r *simRunRepo) FindStuckRuns(_ int64) ([]*workflow.WorkflowRun, error) { return nil, nil }
func (r *simRunRepo) CancelByWorkflow(_ string) (int64, error)               { return 0, nil }
func (r *simRunRepo) CountByWorkflow(_ string) (int64, error)                { return 0, nil }
func (r *simRunRepo) CountActiveByWorkspace(_ string) (int64, error)         { return 0, nil }

type simLogRepo struct {
	mu      sync.Mutex
	logs    []*workflow.WorkflowRunLog
	onWrite func(log *workflow.WorkflowRunLog)
}

func newSimLogRepo(onWrite func(log *workflow.WorkflowRunLog)) *simLogRepo {
	return &simLogRepo{
		logs:    make([]*workflow.WorkflowRunLog, 0),
		onWrite: onWrite,
	}
}

func (r *simLogRepo) Create(logEntry *workflow.WorkflowRunLog) error {
	r.mu.Lock()
	r.logs = append(r.logs, logEntry)
	cb := r.onWrite
	r.mu.Unlock()
	if cb != nil {
		cb(logEntry)
	}
	return nil
}

func (r *simLogRepo) FindByRunID(_ string) ([]*workflow.WorkflowRunLog, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.logs, nil
}

func (r *simLogRepo) CountByRun(_ string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int64(len(r.logs)), nil
}

type simSubWorkflowRunner struct{}

func (r *simSubWorkflowRunner) RunSubWorkflow(_, _, _, _ string, state workflow.RunState) (*workflow.RunState, error) {
	return &state, nil
}
