package coexistence_usecase

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vozko/domain/coexistence"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/messaging"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	whatsappcampaign "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type mockQueueSub struct {
	subscribedTopic string
	handler         func([]byte, messaging.MessageAck)
}

func (m *mockQueueSub) Subscribe(topic string, handler func([]byte, messaging.MessageAck)) error {
	m.subscribedTopic = topic
	m.handler = handler
	return nil
}
func (m *mockQueueSub) DeleteQueue(string) error           { return nil }
func (m *mockQueueSub) ValidateConnection() error          { return nil }
func (m *mockQueueSub) GetQueueLength(string) (int, error) { return 0, nil }

type mockAck struct{ acked, nacked bool }

func (a *mockAck) Ack() error         { a.acked = true; return nil }
func (a *mockAck) Nack(bool) error    { a.nacked = true; return nil }
func (a *mockAck) DeliveryCount() int { return 1 }

type mockBusinessPhoneRepo struct {
	phones map[string]*businessphone.WhatsAppBusinessPhoneNumber
}

func (r *mockBusinessPhoneRepo) FindByMetaPhoneNumberID(metaID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if p, ok := r.phones[metaID]; ok {
		return p, nil
	}
	return nil, errors.New("not found")
}

func (r *mockBusinessPhoneRepo) Create(*businessphone.WhatsAppBusinessPhoneNumber) error { return nil }
func (r *mockBusinessPhoneRepo) Update(string, *businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (r *mockBusinessPhoneRepo) Delete(string) error { return nil }
func (r *mockBusinessPhoneRepo) FindByID(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (r *mockBusinessPhoneRepo) FindByDisplayPhoneNumber(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (r *mockBusinessPhoneRepo) FindByWABAId(string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (r *mockBusinessPhoneRepo) List(businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	return nil, nil
}
func (r *mockBusinessPhoneRepo) ListAll() ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (r *mockBusinessPhoneRepo) BatchUpdate([]*businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (r *mockBusinessPhoneRepo) UpdateStatus(string, businessphone.Status) error { return nil }
func (r *mockBusinessPhoneRepo) UpdateBusinessProfile(string, businessphone.BusinessProfile) error {
	return nil
}
func (r *mockBusinessPhoneRepo) SyncFromMeta(*businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (r *mockBusinessPhoneRepo) ClearAccessToken(string) error { return nil }
func (r *mockBusinessPhoneRepo) ClearOwner(string) error       { return nil }
func (r *mockBusinessPhoneRepo) FindByMetaPhoneNumberIDUnscoped(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, businessphone.ErrPhoneNumberNotFound
}
func (r *mockBusinessPhoneRepo) Restore(string) error { return nil }

type mockCampaignRepo struct {
	campaigns map[string]*whatsappcampaign.Campaign
	created   []*whatsappcampaign.Campaign
}

func (r *mockCampaignRepo) FindLatestOrganicByBusinessPhone(workspaceID, phoneID string) (*whatsappcampaign.Campaign, error) {
	if c, ok := r.campaigns[workspaceID+":"+phoneID]; ok {
		return c, nil
	}
	return nil, errors.New("not found")
}

func (r *mockCampaignRepo) Create(c *whatsappcampaign.Campaign) error {
	r.created = append(r.created, c)
	return nil
}

func (r *mockCampaignRepo) Update(string, *whatsappcampaign.Campaign) error     { return nil }
func (r *mockCampaignRepo) Delete(string) error                                 { return nil }
func (r *mockCampaignRepo) FindByID(string) (*whatsappcampaign.Campaign, error) { return nil, nil }
func (r *mockCampaignRepo) List(whatsappcampaign.ListCampaignsInput) (*shared.PaginatedResult[*whatsappcampaign.Campaign], error) {
	return nil, nil
}
func (r *mockCampaignRepo) ListByStatus(whatsappcampaign.Status) ([]*whatsappcampaign.Campaign, error) {
	return nil, nil
}
func (r *mockCampaignRepo) ListScheduledToStart(time.Time, int) ([]*whatsappcampaign.Campaign, error) {
	return nil, nil
}
func (r *mockCampaignRepo) UpdateStatus(string, whatsappcampaign.Status, ...whatsappcampaign.Status) (bool, error) {
	return false, nil
}
func (r *mockCampaignRepo) UpdateResetCode(string, string) error { return nil }
func (r *mockCampaignRepo) UpdateClearCode(string, string) error { return nil }

type mockEntryRepo struct {
	entries map[string]*wce.WhatsAppCampaignEntry
	created []*wce.WhatsAppCampaignEntry
}

func (r *mockEntryRepo) FindByCampaignAndLead(campaignID, leadID string) (*wce.WhatsAppCampaignEntry, error) {
	if e, ok := r.entries[campaignID+":"+leadID]; ok {
		return e, nil
	}
	return nil, errors.New("not found")
}

func (r *mockEntryRepo) Create(e *wce.WhatsAppCampaignEntry) error {
	r.created = append(r.created, e)
	return nil
}

func (r *mockEntryRepo) CreateMany([]wce.WhatsAppCampaignEntry) ([]wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *mockEntryRepo) FindByID(string) (*wce.WhatsAppCampaignEntry, error) { return nil, nil }
func (r *mockEntryRepo) FindByMessageID(string) (*wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *mockEntryRepo) FindByIDs([]string) ([]*wce.WhatsAppCampaignEntry, error) { return nil, nil }
func (r *mockEntryRepo) FindByCampaignAndNumber(string, string) (*wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *mockEntryRepo) Delete(string) error             { return nil }
func (r *mockEntryRepo) DeleteByCampaignID(string) error { return nil }
func (r *mockEntryRepo) List(wce.ListEntriesInput) (*shared.PaginatedResult[*wce.WhatsAppCampaignEntry], error) {
	return nil, nil
}
func (r *mockEntryRepo) ListByCampaignID(string) ([]wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *mockEntryRepo) ListByLeadID(string) ([]wce.WhatsAppCampaignEntry, error) { return nil, nil }
func (r *mockEntryRepo) ListRecentlyUpdated(string, int) ([]wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *mockEntryRepo) CountByCampaignID(string) (int64, error)         { return 0, nil }
func (r *mockEntryRepo) CountByStatus(string) (*wce.StatusCounts, error) { return nil, nil }
func (r *mockEntryRepo) CountByStatusForCampaigns([]string) (map[string]*wce.StatusCounts, error) {
	return nil, nil
}
func (r *mockEntryRepo) UpdateStatus(string, wce.SendStatus, string, int, string) error { return nil }
func (r *mockEntryRepo) UpdateReceivedBusinessPhone(string, string) error               { return nil }
func (r *mockEntryRepo) UpdateStatusByMessageID(string, wce.SendStatus) error           { return nil }
func (r *mockEntryRepo) UpdateStatusByNumber(string, string, wce.SendStatus, string) error {
	return nil
}
func (r *mockEntryRepo) ResetAllStatuses(string) (int64, error)                           { return 0, nil }
func (r *mockEntryRepo) ReplaceCampaignEntries(string, []wce.WhatsAppCampaignEntry) error { return nil }
func (r *mockEntryRepo) UpsertCampaignEntries(string, []wce.WhatsAppCampaignEntry) error  { return nil }
func (r *mockEntryRepo) ListByStatus(string, wce.SendStatus, int) ([]wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *mockEntryRepo) ListEntriesWithLeads(wce.ListEntriesInput) (*shared.PaginatedResult[*wce.EntryWithLead], error) {
	return nil, nil
}
func (r *mockEntryRepo) ListEntriesWithLeadsForUser(wce.ListEntriesForUserInput) ([]*wce.EntryWithLead, int64, error) {
	return nil, 0, nil
}
func (r *mockEntryRepo) CanUserAccessEntry(string, string, bool) (bool, error)   { return false, nil }
func (r *mockEntryRepo) GetAccessibleEntryIDs(string, bool) ([]string, error)    { return nil, nil }
func (r *mockEntryRepo) GetEntryIDsByCampaign(string) ([]string, error)          { return nil, nil }
func (r *mockEntryRepo) FindByNumber(string) (*wce.WhatsAppCampaignEntry, error) { return nil, nil }
func (r *mockEntryRepo) FindByNumberAndBusinessPhone(string, string) (*wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *mockEntryRepo) GetCampaignForEntry(string) (*wce.EntryCampaignInfo, error) { return nil, nil }
func (r *mockEntryRepo) UpdateAutomationEnabled(string, *bool) error                { return nil }
func (r *mockEntryRepo) UpdateMetadata(string, map[string]interface{}) error        { return nil }
func (r *mockEntryRepo) UpdateConversationStatus(string, wce.ConversationStatusWrite) error {
	return nil
}
func (r *mockEntryRepo) ListEligibleForAutoClose(int) ([]wce.AutoCloseCandidate, error) {
	return nil, nil
}
func (r *mockEntryRepo) ListEligibleForMaxAge(int) ([]wce.AutoCloseCandidate, error) { return nil, nil }
func (r *mockEntryRepo) CountByConversationStatus(string) (map[string]int64, error)  { return nil, nil }
func (r *mockEntryRepo) CountByConversationStatusForWorkspace(string) (map[string]int64, error) {
	return nil, nil
}

type mockMessageRepo struct {
	created []*conversation.Message
}

func (r *mockMessageRepo) Create(msg *conversation.Message) error {
	r.created = append(r.created, msg)
	return nil
}

func (r *mockMessageRepo) Update(string, *conversation.Message) error    { return nil }
func (r *mockMessageRepo) Delete(string) error                           { return nil }
func (r *mockMessageRepo) GetByID(string) (*conversation.Message, error) { return nil, nil }
func (r *mockMessageRepo) ListByEntry(string, shared.EntryType) ([]*conversation.Message, error) {
	return nil, nil
}
func (r *mockMessageRepo) ListByEntryPaginated(conversation.ListMessagesInput) ([]*conversation.Message, error) {
	return nil, nil
}
func (r *mockMessageRepo) ListByLeadID(string) ([]*conversation.Message, error)       { return nil, nil }
func (r *mockMessageRepo) MarkAsRead(conversation.MarkAsReadInput) (int64, error)     { return 0, nil }
func (r *mockMessageRepo) CountUnreadByEntry(string, shared.EntryType) (int64, error) { return 0, nil }
func (r *mockMessageRepo) CountUnreadByEntries([]string, shared.EntryType) ([]conversation.UnreadCount, error) {
	return nil, nil
}
func (r *mockMessageRepo) DeleteByEntry(string, shared.EntryType) error               { return nil }
func (r *mockMessageRepo) DeleteByCampaignID(string, shared.EntryType) (int64, error) { return 0, nil }
func (r *mockMessageRepo) CountByCampaignID(string, shared.EntryType) (int64, error)  { return 0, nil }
func (r *mockMessageRepo) GetEntriesWithMessages(string, []string, shared.EntryType, int, int, string) ([]conversation.EntryWithLastMessage, int64, error) {
	return nil, 0, nil
}
func (r *mockMessageRepo) SearchEntriesWithMessages(conversation.SearchEntriesInput) ([]conversation.EntryWithLastMessage, int64, error) {
	return nil, 0, nil
}
func (r *mockMessageRepo) SearchEntriesByFilter(conversation.SearchByFilterInput) ([]conversation.EntryWithLastMessage, int64, error) {
	return nil, 0, nil
}
func (r *mockMessageRepo) SearchMessagesByEntry(conversation.SearchMessagesByEntryInput) ([]*conversation.Message, int64, error) {
	return nil, 0, nil
}
func (r *mockMessageRepo) GetEntryLastMessage(string, shared.EntryType) (*conversation.EntryWithLastMessage, error) {
	return nil, nil
}
func (r *mockMessageRepo) CountByEntry(string, shared.EntryType) (int64, error) { return 0, nil }
func (r *mockMessageRepo) CountInboundByEntry(string, shared.EntryType) (int64, error) {
	return 0, nil
}
func (r *mockMessageRepo) GetByWhatsAppMessageID(string) (*conversation.Message, error) {
	return nil, nil
}
func (r *mockMessageRepo) GetByExternalMessageID(shared.EntryType, string) (*conversation.Message, error) {
	return nil, nil
}
func (r *mockMessageRepo) UpdateDeliveryStatus(string, conversation.DeliveryStatus) error { return nil }
func (r *mockMessageRepo) UpdateDeliveryStatusWithReason(string, conversation.DeliveryStatus, int, string) error {
	return nil
}
func (r *mockMessageRepo) ClearAll() error { return nil }

type mockLeadRepo struct {
	leads   map[string]*lead.Lead
	created []*lead.Lead
}

func (r *mockLeadRepo) FindOrCreate(_ string, number string, update lead.LeadUpdate) (*lead.Lead, bool, error) {
	if l, ok := r.leads[number]; ok {
		return l, false, nil
	}
	l := &lead.Lead{ID: "lead-" + number, Number: number, Name: update.Name}
	r.leads[number] = l
	r.created = append(r.created, l)
	return l, true, nil
}

func (r *mockLeadRepo) Create(*lead.Lead) error                          { return nil }
func (r *mockLeadRepo) FindByID(string, string) (*lead.Lead, error)      { return nil, nil }
func (r *mockLeadRepo) FindByIDs(string, []string) ([]*lead.Lead, error) { return nil, nil }
func (r *mockLeadRepo) FindByNumber(string, string) (*lead.Lead, error)  { return nil, nil }
func (r *mockLeadRepo) FindOrCreateMany(string, []lead.BulkLeadInput) (map[string]*lead.Lead, error) {
	return nil, nil
}
func (r *mockLeadRepo) Update(string, string, lead.LeadUpdate) error { return nil }
func (r *mockLeadRepo) Delete(string, string) error                  { return nil }
func (r *mockLeadRepo) List(lead.ListLeadsInput) (*shared.PaginatedResult[*lead.Lead], error) {
	return nil, nil
}
func (r *mockLeadRepo) ListWithSummary(lead.ListLeadsInput) (*shared.PaginatedResult[*lead.LeadWithSummary], error) {
	return nil, nil
}
func (r *mockLeadRepo) Facets(_ lead.ListLeadsInput) (*lead.LeadFacets, error) {
	return &lead.LeadFacets{}, nil
}

func (r *mockLeadRepo) ResolveCampaignNames(_ []string) map[string]string { return nil }

func TestConsumeCoexistenceWebhook_SubscribesToCorrectTopic(t *testing.T) {
	sub := &mockQueueSub{}
	uc := NewConsumeCoexistenceWebhookUseCase(sub, nil, nil, nil, nil, nil)
	if err := uc.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	if sub.subscribedTopic != "webhook.whatsapp.coexistence" {
		t.Errorf("expected topic=webhook.whatsapp.coexistence, got %s", sub.subscribedTopic)
	}
}

func TestHandleHistory_CreatesLeadEntryAndMessages(t *testing.T) {
	phone := &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                 "phone-1",
		MetaPhoneNumberID:  "META_PH_1",
		DisplayPhoneNumber: "5511999999999",
		OwnerWorkspaceID:   "ws-1",
	}
	campaign := &whatsappcampaign.Campaign{
		ID:              "camp-1",
		WorkspaceID:     "ws-1",
		BusinessPhoneID: "phone-1",
		Type:            whatsappcampaign.CampaignTypeOrganic,
	}

	phoneRepo := &mockBusinessPhoneRepo{phones: map[string]*businessphone.WhatsAppBusinessPhoneNumber{"META_PH_1": phone}}
	campaignRepo := &mockCampaignRepo{campaigns: map[string]*whatsappcampaign.Campaign{"ws-1:phone-1": campaign}}
	entryRepo := &mockEntryRepo{entries: map[string]*wce.WhatsAppCampaignEntry{}}
	messageRepo := &mockMessageRepo{}
	leadRepo := &mockLeadRepo{leads: map[string]*lead.Lead{}}

	sub := &mockQueueSub{}
	uc := NewConsumeCoexistenceWebhookUseCase(sub, phoneRepo, campaignRepo, entryRepo, messageRepo, leadRepo)
	if err := uc.Start(); err != nil {
		t.Fatal(err)
	}

	payload := coexistence.HistoryWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []coexistence.HistoryWebhookEntry{{
			ID: "WABA_1",
			Changes: []coexistence.HistoryWebhookChange{{
				Field: "history",
				Value: coexistence.HistoryWebhookValue{
					MessagingProduct: "whatsapp",
					Metadata: coexistence.HistoryMetadata{
						DisplayPhoneNumber: "5511999999999",
						PhoneNumberID:      "META_PH_1",
					},
					History: []coexistence.HistoryChunk{{
						Metadata: coexistence.HistoryChunkMetadata{Phase: 0, ChunkOrder: 1, Progress: 50},
						Threads: []coexistence.HistoryThread{{
							ID: "5511888888888",
							Messages: []coexistence.HistoryMessage{
								{
									From:      "5511888888888",
									ID:        "wamid.msg1",
									Timestamp: "1700000000",
									Type:      "text",
									Status:    "DELIVERED",
									Text:      &coexistence.HistoryTextContent{Body: "Hello!"},
								},
								{
									From:      "5511999999999",
									ID:        "wamid.msg2",
									Timestamp: "1700000060",
									Type:      "text",
									Status:    "READ",
									Text:      &coexistence.HistoryTextContent{Body: "Hi there!"},
								},
							},
						}},
					}},
				},
			}},
		}},
	}

	raw, _ := json.Marshal(payload)
	ack := &mockAck{}
	sub.handler(raw, ack)

	if !ack.acked {
		t.Error("expected message to be acked")
	}

	if len(leadRepo.created) != 1 {
		t.Fatalf("expected 1 lead, got %d", len(leadRepo.created))
	}
	if leadRepo.created[0].Number != "5511888888888" {
		t.Errorf("lead number = %s, want 5511888888888", leadRepo.created[0].Number)
	}

	if len(entryRepo.created) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entryRepo.created))
	}
	if entryRepo.created[0].CampaignID != "camp-1" {
		t.Errorf("entry campaign = %s, want camp-1", entryRepo.created[0].CampaignID)
	}
	if entryRepo.created[0].ConversationStatus != "finished" {
		t.Errorf("entry conversation_status = %q, want \"finished\" (synced entries should not appear as new)", entryRepo.created[0].ConversationStatus)
	}

	if len(messageRepo.created) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messageRepo.created))
	}

	msg0 := messageRepo.created[0]
	if msg0.From != "5511888888888" {
		t.Errorf("msg0 from = %s, want 5511888888888", msg0.From)
	}
	if msg0.MessageType != conversation.MessageTypeUserMessage {
		t.Errorf("msg0 type = %s, want user_message", msg0.MessageType)
	}
	if msg0.Text != "Hello!" {
		t.Errorf("msg0 text = %q, want Hello!", msg0.Text)
	}
	if msg0.DeliveryStatus != conversation.DeliveryStatusDelivered {
		t.Errorf("msg0 delivery = %s, want delivered", msg0.DeliveryStatus)
	}
	if msg0.Channel != conversation.MessageChannelWhatsApp {
		t.Errorf("msg0 channel = %s, want whatsapp", msg0.Channel)
	}
	if msg0.EntryType != shared.EntryTypeWhatsApp {
		t.Errorf("msg0 entry_type = %s, want whatsapp", msg0.EntryType)
	}

	msg1 := messageRepo.created[1]
	if msg1.MessageType != conversation.MessageTypeOperator {
		t.Errorf("msg1 type = %s, want operator", msg1.MessageType)
	}
	if msg1.Text != "Hi there!" {
		t.Errorf("msg1 text = %q, want Hi there!", msg1.Text)
	}
	if msg1.DeliveryStatus != conversation.DeliveryStatusRead {
		t.Errorf("msg1 delivery = %s, want read", msg1.DeliveryStatus)
	}
	if msg1.From != "5511999999999" {
		t.Errorf("msg1 from = %s, want 5511999999999", msg1.From)
	}
	if msg1.To != "5511888888888" {
		t.Errorf("msg1 to = %s, want 5511888888888", msg1.To)
	}
}

func TestHandleHistory_MultipleThreads(t *testing.T) {
	phone := &businessphone.WhatsAppBusinessPhoneNumber{
		ID: "phone-1", MetaPhoneNumberID: "META_PH_1",
		DisplayPhoneNumber: "5511999999999", OwnerWorkspaceID: "ws-1",
	}
	campaign := &whatsappcampaign.Campaign{
		ID: "camp-1", WorkspaceID: "ws-1", BusinessPhoneID: "phone-1",
	}

	phoneRepo := &mockBusinessPhoneRepo{phones: map[string]*businessphone.WhatsAppBusinessPhoneNumber{"META_PH_1": phone}}
	campaignRepo := &mockCampaignRepo{campaigns: map[string]*whatsappcampaign.Campaign{"ws-1:phone-1": campaign}}
	entryRepo := &mockEntryRepo{entries: map[string]*wce.WhatsAppCampaignEntry{}}
	messageRepo := &mockMessageRepo{}
	leadRepo := &mockLeadRepo{leads: map[string]*lead.Lead{}}

	sub := &mockQueueSub{}
	uc := NewConsumeCoexistenceWebhookUseCase(sub, phoneRepo, campaignRepo, entryRepo, messageRepo, leadRepo)
	_ = uc.Start()

	payload := coexistence.HistoryWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []coexistence.HistoryWebhookEntry{{
			ID: "WABA_1",
			Changes: []coexistence.HistoryWebhookChange{{
				Field: "history",
				Value: coexistence.HistoryWebhookValue{
					MessagingProduct: "whatsapp",
					Metadata:         coexistence.HistoryMetadata{PhoneNumberID: "META_PH_1"},
					History: []coexistence.HistoryChunk{{
						Metadata: coexistence.HistoryChunkMetadata{Phase: 0, ChunkOrder: 1, Progress: 100},
						Threads: []coexistence.HistoryThread{
							{
								ID: "5511111111111",
								Messages: []coexistence.HistoryMessage{
									{From: "5511111111111", ID: "m1", Timestamp: "1700000000", Type: "text",
										Text: &coexistence.HistoryTextContent{Body: "Thread 1"}},
								},
							},
							{
								ID: "5522222222222",
								Messages: []coexistence.HistoryMessage{
									{From: "5522222222222", ID: "m2", Timestamp: "1700000001", Type: "text",
										Text: &coexistence.HistoryTextContent{Body: "Thread 2"}},
								},
							},
						},
					}},
				},
			}},
		}},
	}

	raw, _ := json.Marshal(payload)
	ack := &mockAck{}
	sub.handler(raw, ack)

	if len(leadRepo.created) != 2 {
		t.Fatalf("expected 2 leads, got %d", len(leadRepo.created))
	}
	if len(entryRepo.created) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entryRepo.created))
	}
	if len(messageRepo.created) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messageRepo.created))
	}
}

func TestHandleHistory_UnknownPhone_NoMessages(t *testing.T) {
	phoneRepo := &mockBusinessPhoneRepo{phones: map[string]*businessphone.WhatsAppBusinessPhoneNumber{}}
	messageRepo := &mockMessageRepo{}

	sub := &mockQueueSub{}
	uc := NewConsumeCoexistenceWebhookUseCase(sub, phoneRepo, nil, nil, messageRepo, nil)
	_ = uc.Start()

	payload := coexistence.HistoryWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []coexistence.HistoryWebhookEntry{{
			ID: "WABA_1",
			Changes: []coexistence.HistoryWebhookChange{{
				Field: "history",
				Value: coexistence.HistoryWebhookValue{
					Metadata: coexistence.HistoryMetadata{PhoneNumberID: "UNKNOWN"},
					History:  []coexistence.HistoryChunk{},
				},
			}},
		}},
	}

	raw, _ := json.Marshal(payload)
	ack := &mockAck{}
	sub.handler(raw, ack)

	if !ack.acked {
		t.Error("expected ack even on unknown phone")
	}
	if len(messageRepo.created) != 0 {
		t.Error("should not create messages for unknown phone")
	}
}

func TestHandleHistory_NoCampaign_NoMessages(t *testing.T) {
	phone := &businessphone.WhatsAppBusinessPhoneNumber{
		ID: "phone-1", MetaPhoneNumberID: "META_PH_1", OwnerWorkspaceID: "ws-1",
	}
	phoneRepo := &mockBusinessPhoneRepo{phones: map[string]*businessphone.WhatsAppBusinessPhoneNumber{"META_PH_1": phone}}
	campaignRepo := &mockCampaignRepo{campaigns: map[string]*whatsappcampaign.Campaign{}}
	messageRepo := &mockMessageRepo{}

	sub := &mockQueueSub{}
	uc := NewConsumeCoexistenceWebhookUseCase(sub, phoneRepo, campaignRepo, nil, messageRepo, nil)
	_ = uc.Start()

	payload := coexistence.HistoryWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []coexistence.HistoryWebhookEntry{{
			Changes: []coexistence.HistoryWebhookChange{{
				Field: "history",
				Value: coexistence.HistoryWebhookValue{
					Metadata: coexistence.HistoryMetadata{PhoneNumberID: "META_PH_1"},
					History: []coexistence.HistoryChunk{{
						Threads: []coexistence.HistoryThread{{
							ID:       "5511888888888",
							Messages: []coexistence.HistoryMessage{{From: "5511888888888", Type: "text"}},
						}},
					}},
				},
			}},
		}},
	}

	raw, _ := json.Marshal(payload)
	ack := &mockAck{}
	sub.handler(raw, ack)

	if len(messageRepo.created) != 0 {
		t.Error("should not create messages when no organic campaign exists")
	}
}

func TestHandleHistory_ImageMessage(t *testing.T) {
	phone := &businessphone.WhatsAppBusinessPhoneNumber{
		ID: "phone-1", MetaPhoneNumberID: "META_PH_1",
		DisplayPhoneNumber: "5511999999999", OwnerWorkspaceID: "ws-1",
	}
	campaign := &whatsappcampaign.Campaign{ID: "camp-1", WorkspaceID: "ws-1", BusinessPhoneID: "phone-1"}

	phoneRepo := &mockBusinessPhoneRepo{phones: map[string]*businessphone.WhatsAppBusinessPhoneNumber{"META_PH_1": phone}}
	campaignRepo := &mockCampaignRepo{campaigns: map[string]*whatsappcampaign.Campaign{"ws-1:phone-1": campaign}}
	entryRepo := &mockEntryRepo{entries: map[string]*wce.WhatsAppCampaignEntry{}}
	messageRepo := &mockMessageRepo{}
	leadRepo := &mockLeadRepo{leads: map[string]*lead.Lead{}}

	sub := &mockQueueSub{}
	uc := NewConsumeCoexistenceWebhookUseCase(sub, phoneRepo, campaignRepo, entryRepo, messageRepo, leadRepo)
	_ = uc.Start()

	payload := coexistence.HistoryWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []coexistence.HistoryWebhookEntry{{
			Changes: []coexistence.HistoryWebhookChange{{
				Field: "history",
				Value: coexistence.HistoryWebhookValue{
					Metadata: coexistence.HistoryMetadata{PhoneNumberID: "META_PH_1"},
					History: []coexistence.HistoryChunk{{
						Threads: []coexistence.HistoryThread{{
							ID: "5511888888888",
							Messages: []coexistence.HistoryMessage{{
								From: "5511888888888", ID: "wamid.img1", Timestamp: "1700000000",
								Type: "image", Status: "DELIVERED",
								Image: &coexistence.HistoryMediaContent{
									ID: "media-123", MimeType: "image/jpeg", Caption: "Check this out",
								},
							}},
						}},
					}},
				},
			}},
		}},
	}

	raw, _ := json.Marshal(payload)
	sub.handler(raw, &mockAck{})

	if len(messageRepo.created) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messageRepo.created))
	}
	msg := messageRepo.created[0]
	if msg.MessageType != conversation.MessageTypeMedia {
		t.Errorf("type = %s, want media", msg.MessageType)
	}
	if msg.MediaType != conversation.MediaTypeImage {
		t.Errorf("media_type = %s, want image", msg.MediaType)
	}
	if msg.Text != "Check this out" {
		t.Errorf("text = %q, want 'Check this out'", msg.Text)
	}
}

func TestHandleStateSync_CreatesLeads(t *testing.T) {
	leadRepo := &mockLeadRepo{leads: map[string]*lead.Lead{}}
	phone := &businessphone.WhatsAppBusinessPhoneNumber{ID: "bp-1", MetaPhoneNumberID: "META_PH_1", OwnerWorkspaceID: "ws-1"}
	phoneRepo := &mockBusinessPhoneRepo{phones: map[string]*businessphone.WhatsAppBusinessPhoneNumber{"META_PH_1": phone}}

	sub := &mockQueueSub{}
	uc := NewConsumeCoexistenceWebhookUseCase(sub, phoneRepo, nil, nil, nil, leadRepo)
	_ = uc.Start()

	payload := coexistence.SMBStateSyncWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []coexistence.SMBStateSyncEntry{{
			ID: "WABA_1",
			Changes: []coexistence.SMBStateSyncChange{{
				Field: "smb_app_state_sync",
				Value: coexistence.SMBStateSyncValue{
					MessagingProduct: "whatsapp",
					Metadata:         coexistence.HistoryMetadata{PhoneNumberID: "META_PH_1"},
					StateSync: []coexistence.SMBStateSyncItem{
						{
							Type:   "contact",
							Action: "add",
							Contact: coexistence.SMBContact{
								FullName:    "Maria Silva",
								FirstName:   "Maria",
								PhoneNumber: "5511777777777",
							},
						},
						{
							Type:   "contact",
							Action: "add",
							Contact: coexistence.SMBContact{
								FullName:    "João Santos",
								FirstName:   "João",
								PhoneNumber: "5511666666666",
							},
						},
					},
				},
			}},
		}},
	}

	raw, _ := json.Marshal(payload)
	ack := &mockAck{}
	sub.handler(raw, ack)

	if !ack.acked {
		t.Error("expected ack")
	}
	if len(leadRepo.created) != 2 {
		t.Fatalf("expected 2 leads, got %d", len(leadRepo.created))
	}
	if leadRepo.created[0].Name != "Maria Silva" {
		t.Errorf("lead[0].Name = %q, want 'Maria Silva'", leadRepo.created[0].Name)
	}
	if leadRepo.created[1].Number != "5511666666666" {
		t.Errorf("lead[1].Number = %q, want '5511666666666'", leadRepo.created[1].Number)
	}
}

func TestHandleStateSync_SkipsRemoveAction(t *testing.T) {
	leadRepo := &mockLeadRepo{leads: map[string]*lead.Lead{}}

	sub := &mockQueueSub{}
	uc := NewConsumeCoexistenceWebhookUseCase(sub, nil, nil, nil, nil, leadRepo)
	_ = uc.Start()

	payload := coexistence.SMBStateSyncWebhookPayload{
		Entry: []coexistence.SMBStateSyncEntry{{
			Changes: []coexistence.SMBStateSyncChange{{
				Value: coexistence.SMBStateSyncValue{
					StateSync: []coexistence.SMBStateSyncItem{
						{Type: "contact", Action: "remove", Contact: coexistence.SMBContact{PhoneNumber: "5511777777777"}},
					},
				},
			}},
		}},
	}

	raw, _ := json.Marshal(payload)
	sub.handler(raw, &mockAck{})

	if len(leadRepo.created) != 0 {
		t.Error("should not create leads for 'remove' action")
	}
}

func TestHandleStateSync_SkipsEmptyPhoneNumber(t *testing.T) {
	leadRepo := &mockLeadRepo{leads: map[string]*lead.Lead{}}

	sub := &mockQueueSub{}
	uc := NewConsumeCoexistenceWebhookUseCase(sub, nil, nil, nil, nil, leadRepo)
	_ = uc.Start()

	payload := coexistence.SMBStateSyncWebhookPayload{
		Entry: []coexistence.SMBStateSyncEntry{{
			Changes: []coexistence.SMBStateSyncChange{{
				Value: coexistence.SMBStateSyncValue{
					StateSync: []coexistence.SMBStateSyncItem{
						{Type: "contact", Action: "add", Contact: coexistence.SMBContact{PhoneNumber: "", FullName: "No Phone"}},
					},
				},
			}},
		}},
	}

	raw, _ := json.Marshal(payload)
	sub.handler(raw, &mockAck{})

	if len(leadRepo.created) != 0 {
		t.Error("should not create lead with empty phone number")
	}
}

func TestHandleMessageEchoes_PersistsOutboundMessages(t *testing.T) {
	phone := &businessphone.WhatsAppBusinessPhoneNumber{
		ID: "phone-1", MetaPhoneNumberID: "META_PH_1",
		DisplayPhoneNumber: "5511999999999", OwnerWorkspaceID: "ws-1",
	}
	campaign := &whatsappcampaign.Campaign{
		ID: "camp-1", WorkspaceID: "ws-1", BusinessPhoneID: "phone-1",
	}

	phoneRepo := &mockBusinessPhoneRepo{phones: map[string]*businessphone.WhatsAppBusinessPhoneNumber{"META_PH_1": phone}}
	campaignRepo := &mockCampaignRepo{campaigns: map[string]*whatsappcampaign.Campaign{"ws-1:phone-1": campaign}}
	entryRepo := &mockEntryRepo{entries: map[string]*wce.WhatsAppCampaignEntry{}}
	messageRepo := &mockMessageRepo{}
	leadRepo := &mockLeadRepo{leads: map[string]*lead.Lead{}}

	sub := &mockQueueSub{}
	uc := NewConsumeCoexistenceWebhookUseCase(sub, phoneRepo, campaignRepo, entryRepo, messageRepo, leadRepo)
	_ = uc.Start()

	payload := coexistence.SMBMessageEchoesWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []coexistence.SMBMessageEchoesEntry{{
			ID: "WABA_1",
			Changes: []coexistence.SMBMessageEchoesChange{{
				Field: "smb_message_echoes",
				Value: coexistence.SMBMessageEchoesValue{
					MessagingProduct: "whatsapp",
					Metadata:         coexistence.HistoryMetadata{PhoneNumberID: "META_PH_1"},
					MessageEchoes: []coexistence.SMBMessageEcho{{
						From:      "5511999999999",
						To:        "5511888888888",
						ID:        "wamid.echo1",
						Timestamp: "1700000000",
						Type:      "text",
						Text:      &coexistence.HistoryTextContent{Body: "Follow-up message"},
					}},
				},
			}},
		}},
	}

	raw, _ := json.Marshal(payload)
	ack := &mockAck{}
	sub.handler(raw, ack)

	if !ack.acked {
		t.Error("expected ack")
	}

	if len(leadRepo.created) != 1 {
		t.Fatalf("expected 1 lead, got %d", len(leadRepo.created))
	}
	if len(entryRepo.created) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entryRepo.created))
	}
	if len(messageRepo.created) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messageRepo.created))
	}

	msg := messageRepo.created[0]
	if msg.MessageType != conversation.MessageTypeOperator {
		t.Errorf("type = %s, want operator", msg.MessageType)
	}
	if msg.Text != "Follow-up message" {
		t.Errorf("text = %q, want 'Follow-up message'", msg.Text)
	}
	if msg.From != "5511999999999" {
		t.Errorf("from = %s, want 5511999999999", msg.From)
	}
	if msg.To != "5511888888888" {
		t.Errorf("to = %s, want 5511888888888", msg.To)
	}
	if msg.DeliveryStatus != conversation.DeliveryStatusSent {
		t.Errorf("delivery = %s, want sent", msg.DeliveryStatus)
	}
}

func TestHandleMessageEchoes_SkipsEmptyToField(t *testing.T) {
	phone := &businessphone.WhatsAppBusinessPhoneNumber{
		ID: "phone-1", MetaPhoneNumberID: "META_PH_1", OwnerWorkspaceID: "ws-1",
	}
	campaign := &whatsappcampaign.Campaign{ID: "camp-1", WorkspaceID: "ws-1", BusinessPhoneID: "phone-1"}

	phoneRepo := &mockBusinessPhoneRepo{phones: map[string]*businessphone.WhatsAppBusinessPhoneNumber{"META_PH_1": phone}}
	campaignRepo := &mockCampaignRepo{campaigns: map[string]*whatsappcampaign.Campaign{"ws-1:phone-1": campaign}}
	messageRepo := &mockMessageRepo{}

	sub := &mockQueueSub{}
	uc := NewConsumeCoexistenceWebhookUseCase(sub, phoneRepo, campaignRepo, nil, messageRepo, nil)
	_ = uc.Start()

	payload := coexistence.SMBMessageEchoesWebhookPayload{
		Entry: []coexistence.SMBMessageEchoesEntry{{
			Changes: []coexistence.SMBMessageEchoesChange{{
				Value: coexistence.SMBMessageEchoesValue{
					Metadata:      coexistence.HistoryMetadata{PhoneNumberID: "META_PH_1"},
					MessageEchoes: []coexistence.SMBMessageEcho{{From: "5511999999999", To: "", Type: "text"}},
				},
			}},
		}},
	}

	raw, _ := json.Marshal(payload)
	sub.handler(raw, &mockAck{})

	if len(messageRepo.created) != 0 {
		t.Error("should not process echo with empty To field")
	}
}

func TestHandleUnknownField_AcksWithoutPanic(t *testing.T) {
	sub := &mockQueueSub{}
	uc := NewConsumeCoexistenceWebhookUseCase(sub, nil, nil, nil, nil, nil)
	_ = uc.Start()

	raw := []byte(`{"entry":[{"changes":[{"field":"unknown_field","value":{}}]}]}`)
	ack := &mockAck{}
	sub.handler(raw, ack)

	if !ack.acked {
		t.Error("expected ack for unknown field")
	}
}

func TestHandleInvalidJSON_AcksWithoutPanic(t *testing.T) {
	sub := &mockQueueSub{}
	uc := NewConsumeCoexistenceWebhookUseCase(sub, nil, nil, nil, nil, nil)
	_ = uc.Start()

	ack := &mockAck{}
	sub.handler([]byte("not json at all"), ack)

	if !ack.acked {
		t.Error("expected ack even for invalid JSON")
	}
}

func TestClassifyHistoryMessage_AllTypes(t *testing.T) {
	businessPhone := &businessphone.WhatsAppBusinessPhoneNumber{DisplayPhoneNumber: "5511999999999"}

	tests := []struct {
		name          string
		msg           coexistence.HistoryMessage
		wantMsgType   conversation.MessageType
		wantMediaType conversation.MediaType
	}{
		{
			name:        "user text",
			msg:         coexistence.HistoryMessage{From: "5511888888888", Type: "text", Text: &coexistence.HistoryTextContent{Body: "Hello"}},
			wantMsgType: conversation.MessageTypeUserMessage,
		},
		{
			name:        "business text (from synced phone)",
			msg:         coexistence.HistoryMessage{From: "5511999999999", Type: "text", Text: &coexistence.HistoryTextContent{Body: "Reply"}},
			wantMsgType: conversation.MessageTypeOperator,
		},
		{
			name:          "user image",
			msg:           coexistence.HistoryMessage{From: "5511888888888", Type: "image"},
			wantMsgType:   conversation.MessageTypeMedia,
			wantMediaType: conversation.MediaTypeImage,
		},
		{
			name:          "user audio",
			msg:           coexistence.HistoryMessage{From: "5511888888888", Type: "audio"},
			wantMsgType:   conversation.MessageTypeAudio,
			wantMediaType: conversation.MediaTypeAudio,
		},
		{
			name:          "user video",
			msg:           coexistence.HistoryMessage{From: "5511888888888", Type: "video"},
			wantMsgType:   conversation.MessageTypeMedia,
			wantMediaType: conversation.MediaTypeVideo,
		},
		{
			name:          "user document",
			msg:           coexistence.HistoryMessage{From: "5511888888888", Type: "document"},
			wantMsgType:   conversation.MessageTypeMedia,
			wantMediaType: conversation.MediaTypeDocument,
		},
		{
			name:          "user sticker",
			msg:           coexistence.HistoryMessage{From: "5511888888888", Type: "sticker"},
			wantMsgType:   conversation.MessageTypeMedia,
			wantMediaType: conversation.MediaTypeSticker,
		},
		{
			name:        "media_placeholder → system",
			msg:         coexistence.HistoryMessage{From: "5511888888888", Type: "media_placeholder"},
			wantMsgType: conversation.MessageTypeSystem,
		},
		{
			name:        "location",
			msg:         coexistence.HistoryMessage{From: "5511888888888", Type: "location", Location: &coexistence.HistoryLocation{Name: "Office"}},
			wantMsgType: conversation.MessageTypeUserMessage,
		},
		{
			name:        "contacts",
			msg:         coexistence.HistoryMessage{From: "5511888888888", Type: "contacts", Contacts: []coexistence.HistoryContact{{Name: coexistence.HistoryContactName{FormattedName: "Joe"}}}},
			wantMsgType: conversation.MessageTypeUserMessage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMsgType, gotMediaType, _ := classifyHistoryMessage(tt.msg, businessPhone)
			if gotMsgType != tt.wantMsgType {
				t.Errorf("msgType = %s, want %s", gotMsgType, tt.wantMsgType)
			}
			if gotMediaType != tt.wantMediaType {
				t.Errorf("mediaType = %s, want %s", gotMediaType, tt.wantMediaType)
			}
		})
	}
}

func TestMapHistoryStatus(t *testing.T) {
	tests := []struct {
		input string
		want  conversation.DeliveryStatus
	}{
		{"SENT", conversation.DeliveryStatusSent},
		{"DELIVERED", conversation.DeliveryStatusDelivered},
		{"READ", conversation.DeliveryStatusRead},
		{"FAILED", conversation.DeliveryStatusFailed},
		{"", conversation.DeliveryStatusNone},
		{"UNKNOWN", conversation.DeliveryStatusNone},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := mapHistoryStatus(tt.input); got != tt.want {
				t.Errorf("mapHistoryStatus(%q) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractField(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{"history", `{"entry":[{"changes":[{"field":"history"}]}]}`, "history"},
		{"state_sync", `{"entry":[{"changes":[{"field":"smb_app_state_sync"}]}]}`, "smb_app_state_sync"},
		{"echoes", `{"entry":[{"changes":[{"field":"smb_message_echoes"}]}]}`, "smb_message_echoes"},
		{"empty", `{}`, ""},
		{"invalid json", `not json`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractField([]byte(tt.payload)); got != tt.want {
				t.Errorf("extractField() = %q, want %q", got, tt.want)
			}
		})
	}
}

func (r *mockBusinessPhoneRepo) UpdateCallsEnabled(string, bool) error { return nil }
