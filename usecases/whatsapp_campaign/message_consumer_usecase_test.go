package whatsapp_campaign_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/business_metrics"
	"vozko/domain/cache"
	"vozko/domain/conversation"
	"vozko/domain/lead_campaign_send"
	"vozko/domain/messaging"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	wsc "vozko/domain/workspace_config"
)

type mockMessageQueueSub struct {
	mu           sync.Mutex
	handlers     map[string]func([]byte, messaging.MessageAck)
	deleted      map[string]bool
	queueLengths map[string]int
}

func newMockQueueSub() *mockMessageQueueSub {
	return &mockMessageQueueSub{
		handlers:     make(map[string]func([]byte, messaging.MessageAck)),
		deleted:      make(map[string]bool),
		queueLengths: make(map[string]int),
	}
}

func (m *mockMessageQueueSub) Subscribe(topic string, handler func([]byte, messaging.MessageAck)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[topic] = handler
	delete(m.deleted, topic)
	return nil
}

func (m *mockMessageQueueSub) DeleteQueue(topic string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.handlers, topic)
	m.deleted[topic] = true
	return nil
}

func (m *mockMessageQueueSub) StopConsumer(topic string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.handlers, topic)
	return nil
}

func (m *mockMessageQueueSub) ValidateConnection() error { return nil }

func (m *mockMessageQueueSub) GetQueueLength(topic string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l, ok := m.queueLengths[topic]; ok {
		return l, nil
	}

	return 1, nil
}

func (m *mockMessageQueueSub) deliver(topic string, payload []byte) *mockAck {
	m.mu.Lock()
	handler, ok := m.handlers[topic]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	ack := &mockAck{}
	handler(payload, ack)
	return ack
}

func (m *mockMessageQueueSub) isSubscribed(topic string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.handlers[topic]
	return ok
}

func (m *mockMessageQueueSub) isDeleted(topic string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deleted[topic]
}

type delayedMessage struct {
	payload []byte
	delay   time.Duration
}

type mockMessageQueuePub struct {
	mu              sync.Mutex
	messages        map[string][][]byte
	delayedMessages map[string][]delayedMessage
}

func newMockQueuePub() *mockMessageQueuePub {
	return &mockMessageQueuePub{
		messages:        make(map[string][][]byte),
		delayedMessages: make(map[string][]delayedMessage),
	}
}

func (m *mockMessageQueuePub) Publish(topic string, message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[topic] = append(m.messages[topic], message)
	return nil
}

func (m *mockMessageQueuePub) PublishWithDelay(topic string, message []byte, delay time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.delayedMessages[topic] = append(m.delayedMessages[topic], delayedMessage{payload: message, delay: delay})
	return nil
}

func (m *mockMessageQueuePub) ValidateConnection() error { return nil }

func (m *mockMessageQueuePub) messagesFor(topic string) [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages[topic]
}

func (m *mockMessageQueuePub) delayedMessagesFor(topic string) []delayedMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.delayedMessages[topic]
}

type mockAck struct {
	acked   atomic.Bool
	nacked  atomic.Bool
	requeue atomic.Bool
}

func (a *mockAck) Ack() error {
	a.acked.Store(true)
	return nil
}

func (a *mockAck) Nack(requeue bool) error {
	a.nacked.Store(true)
	a.requeue.Store(requeue)
	return nil
}

func (a *mockAck) DeliveryCount() int { return 1 }

type mockSharedState struct {
	mu   sync.RWMutex
	data map[string]string
	subs map[string]func([]byte)
}

func newMockSharedState() *mockSharedState {
	return &mockSharedState{
		data: make(map[string]string),
		subs: make(map[string]func([]byte)),
	}
}

func (s *mockSharedState) SetNX(key, value string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; ok {
		return false, nil
	}
	s.data[key] = value
	return true, nil
}

func (s *mockSharedState) SetString(key, value string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return nil
}

func (s *mockSharedState) GetString(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return v, nil
}

func (s *mockSharedState) Del(keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		delete(s.data, k)
	}
	return nil
}

func (s *mockSharedState) Exists(key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[key]
	return ok, nil
}

func (s *mockSharedState) Incr(key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cur int64
	if v, ok := s.data[key]; ok {
		cur, _ = strconv.ParseInt(v, 10, 64)
	}
	cur++
	s.data[key] = strconv.FormatInt(cur, 10)
	return cur, nil
}

func (s *mockSharedState) Decr(key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cur int64
	if v, ok := s.data[key]; ok {
		cur, _ = strconv.ParseInt(v, 10, 64)
	}
	cur--
	s.data[key] = strconv.FormatInt(cur, 10)
	return cur, nil
}
func (s *mockSharedState) IncrWithTTL(key string, _ time.Duration) (int64, error) { return 0, nil }
func (s *mockSharedState) TryIncr(key string, _ int64) (bool, error)              { return true, nil }
func (s *mockSharedState) SAdd(key string, _ ...string) error                     { return nil }
func (s *mockSharedState) SRem(key string, _ ...string) error                     { return nil }
func (s *mockSharedState) SMembers(key string) ([]string, error)                  { return nil, nil }
func (s *mockSharedState) HSet(key, field, value string) error                    { return nil }
func (s *mockSharedState) HDel(key, field string) error                           { return nil }
func (s *mockSharedState) HGetAll(key string) (map[string]string, error)          { return nil, nil }
func (s *mockSharedState) HIncrBy(key, field string, incr int64) (int64, error)   { return 0, nil }
func (s *mockSharedState) Expire(key string, _ time.Duration) (bool, error)       { return true, nil }

func (s *mockSharedState) IncrBy(key string, amount int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cur int64
	if v, ok := s.data[key]; ok {
		cur, _ = strconv.ParseInt(v, 10, 64)
	}
	cur += amount
	s.data[key] = strconv.FormatInt(cur, 10)
	return cur, nil
}

func (s *mockSharedState) DecrBy(key string, amount int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cur int64
	if v, ok := s.data[key]; ok {
		cur, _ = strconv.ParseInt(v, 10, 64)
	}
	cur -= amount
	s.data[key] = strconv.FormatInt(cur, 10)
	return cur, nil
}

func (s *mockSharedState) TryIncrBy(key string, delta int64, max int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cur int64
	if v, ok := s.data[key]; ok {
		cur, _ = strconv.ParseInt(v, 10, 64)
	}
	if cur+delta > max {
		return false, nil
	}
	cur += delta
	s.data[key] = strconv.FormatInt(cur, 10)
	return true, nil
}

func (s *mockSharedState) Publish(channel string, data []byte) error {
	s.mu.RLock()
	h, ok := s.subs[channel]
	s.mu.RUnlock()
	if ok {
		h(data)
	}
	return nil
}

func (s *mockSharedState) Subscribe(_ context.Context, channel string, handler func([]byte)) {
	s.mu.Lock()
	s.subs[channel] = handler
	s.mu.Unlock()
}

type mockCampaignRepo struct {
	mu        sync.RWMutex
	campaigns map[string]*wc.Campaign
}

func newMockCampaignRepo() *mockCampaignRepo {
	return &mockCampaignRepo{campaigns: make(map[string]*wc.Campaign)}
}

func (r *mockCampaignRepo) Create(c *wc.Campaign) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.campaigns[c.ID] = c
	return nil
}

func (r *mockCampaignRepo) Update(id string, c *wc.Campaign) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.campaigns[id] = c
	return nil
}

func (r *mockCampaignRepo) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.campaigns, id)
	return nil
}

func (r *mockCampaignRepo) FindByID(id string) (*wc.Campaign, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.campaigns[id]
	if !ok {
		return nil, wc.ErrCampaignNotFound
	}
	return c, nil
}

func (r *mockCampaignRepo) FindLatestOrganicByBusinessPhone(_, _ string) (*wc.Campaign, error) {
	return nil, nil
}

func (r *mockCampaignRepo) List(_ wc.ListCampaignsInput) (*shared.PaginatedResult[*wc.Campaign], error) {
	return nil, nil
}

func (r *mockCampaignRepo) ListByStatus(status wc.Status) ([]*wc.Campaign, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*wc.Campaign
	for _, c := range r.campaigns {
		if c.Status == status {
			result = append(result, c)
		}
	}
	return result, nil
}

func (r *mockCampaignRepo) ListScheduledToStart(_ time.Time, _ int) ([]*wc.Campaign, error) {
	return nil, nil
}

func (r *mockCampaignRepo) UpdateStatus(id string, status wc.Status, _ ...wc.Status) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.campaigns[id]; ok {
		c.Status = status
		return true, nil
	}
	return false, nil
}

func (r *mockCampaignRepo) UpdateResetCode(_ string, _ string) error { return nil }
func (r *mockCampaignRepo) UpdateClearCode(_ string, _ string) error { return nil }

type mockEntryRepo struct {
	mu            sync.RWMutex
	entries       map[string]*wce.WhatsAppCampaignEntry
	statuses      map[string]wce.SendStatus
	errorCodes    map[string]int
	errorMessages map[string]string
	findByIDCalls int64
}

func newMockEntryRepo() *mockEntryRepo {
	return &mockEntryRepo{
		entries:       make(map[string]*wce.WhatsAppCampaignEntry),
		statuses:      make(map[string]wce.SendStatus),
		errorCodes:    make(map[string]int),
		errorMessages: make(map[string]string),
	}
}

func (r *mockEntryRepo) FindByID(id string) (*wce.WhatsAppCampaignEntry, error) {
	atomic.AddInt64(&r.findByIDCalls, 1)
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok {
		return nil, wce.ErrEntryNotFound
	}
	return e, nil
}

func (r *mockEntryRepo) findByIDCount() int64 { return atomic.LoadInt64(&r.findByIDCalls) }

func (r *mockEntryRepo) FindByMessageID(messageID string) (*wce.WhatsAppCampaignEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, entry := range r.entries {
		if entry != nil && entry.MessageID == messageID {
			return entry, nil
		}
	}
	return nil, wce.ErrEntryNotFound
}

func (r *mockEntryRepo) UpdateStatus(entryID string, status wce.SendStatus, _ string, errorCode int, errorMessage string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses[entryID] = status
	r.errorCodes[entryID] = errorCode
	r.errorMessages[entryID] = errorMessage
	if e, ok := r.entries[entryID]; ok {
		e.Status = status
		e.ErrorCode = errorCode
		e.ErrorMessage = errorMessage
	}
	return nil
}

func (r *mockEntryRepo) UpdateMetadata(entryID string, metadata map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[entryID]; ok {
		e.Metadata = metadata
	}
	return nil
}

func (r *mockEntryRepo) getStatus(entryID string) wce.SendStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.statuses[entryID]
}

func (r *mockEntryRepo) getErrorCode(entryID string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.errorCodes[entryID]
}

func (r *mockEntryRepo) getErrorMessage(entryID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.errorMessages[entryID]
}

func (r *mockEntryRepo) Create(_ *wce.WhatsAppCampaignEntry) error { return nil }
func (r *mockEntryRepo) CreateMany(_ []wce.WhatsAppCampaignEntry) ([]wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *mockEntryRepo) FindByIDs(_ []string) ([]*wce.WhatsAppCampaignEntry, error) { return nil, nil }
func (r *mockEntryRepo) FindByCampaignAndLead(_, _ string) (*wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *mockEntryRepo) FindByCampaignAndNumber(_, _ string) (*wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *mockEntryRepo) Delete(_ string) error             { return nil }
func (r *mockEntryRepo) DeleteByCampaignID(_ string) error { return nil }
func (r *mockEntryRepo) List(_ wce.ListEntriesInput) (*shared.PaginatedResult[*wce.WhatsAppCampaignEntry], error) {
	return nil, nil
}
func (r *mockEntryRepo) ListByCampaignID(_ string) ([]wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *mockEntryRepo) ListByLeadID(_ string) ([]wce.WhatsAppCampaignEntry, error) { return nil, nil }
func (r *mockEntryRepo) ListRecentlyUpdated(_ string, _ int) ([]wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *mockEntryRepo) CountByCampaignID(_ string) (int64, error)                { return 0, nil }
func (r *mockEntryRepo) CountByStatus(_ string) (*wce.StatusCounts, error)        { return nil, nil }
func (r *mockEntryRepo) CountByStatusForCampaigns(_ []string) (map[string]*wce.StatusCounts, error) {
	return nil, nil
}
func (r *mockEntryRepo) UpdateReceivedBusinessPhone(_, _ string) error            { return nil }
func (r *mockEntryRepo) UpdateStatusByMessageID(_ string, _ wce.SendStatus) error { return nil }
func (r *mockEntryRepo) UpdateStatusByNumber(_, _ string, _ wce.SendStatus, _ string) error {
	return nil
}
func (r *mockEntryRepo) ResetAllStatuses(_ string) (int64, error) { return 0, nil }
func (r *mockEntryRepo) ReplaceCampaignEntries(_ string, _ []wce.WhatsAppCampaignEntry) error {
	return nil
}
func (r *mockEntryRepo) UpsertCampaignEntries(_ string, _ []wce.WhatsAppCampaignEntry) error {
	return nil
}
func (r *mockEntryRepo) ListByStatus(_ string, _ wce.SendStatus, _ int) ([]wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *mockEntryRepo) ListEntriesWithLeads(_ wce.ListEntriesInput) (*shared.PaginatedResult[*wce.EntryWithLead], error) {
	return nil, nil
}
func (r *mockEntryRepo) ListEntriesWithLeadsForUser(_ wce.ListEntriesForUserInput) ([]*wce.EntryWithLead, int64, error) {
	return nil, 0, nil
}
func (r *mockEntryRepo) CanUserAccessEntry(_, _ string, _ bool) (bool, error)      { return true, nil }
func (r *mockEntryRepo) GetAccessibleEntryIDs(_ string, _ bool) ([]string, error)  { return nil, nil }
func (r *mockEntryRepo) GetEntryIDsByCampaign(_ string) ([]string, error)          { return nil, nil }
func (r *mockEntryRepo) FindByNumber(_ string) (*wce.WhatsAppCampaignEntry, error) { return nil, nil }
func (r *mockEntryRepo) FindByNumberAndBusinessPhone(_, _ string) (*wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *mockEntryRepo) GetCampaignForEntry(_ string) (*wce.EntryCampaignInfo, error) {
	return nil, nil
}
func (r *mockEntryRepo) UpdateAutomationEnabled(_ string, _ *bool) error { return nil }
func (r *mockEntryRepo) UpdateConversationStatus(_ string, _ wce.ConversationStatusWrite) error { return nil }
func (r *mockEntryRepo) ListEligibleForAutoClose(int) ([]wce.AutoCloseCandidate, error)         { return nil, nil }
func (r *mockEntryRepo) ListEligibleForMaxAge(int) ([]wce.AutoCloseCandidate, error)         { return nil, nil }
func (r *mockEntryRepo) CountByConversationStatus(_ string) (map[string]int64, error) {
	return nil, nil
}
func (r *mockEntryRepo) CountByConversationStatusForWorkspace(_ string) (map[string]int64, error) {
	return nil, nil
}

type mockTemplateRepo struct {
	mu        sync.RWMutex
	templates map[string]*template.Template
}

func newMockTemplateRepo() *mockTemplateRepo {
	return &mockTemplateRepo{templates: make(map[string]*template.Template)}
}

func (r *mockTemplateRepo) FindByID(id string) (*template.Template, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.templates[id]
	if !ok {
		return nil, fmt.Errorf("template not found")
	}
	return t, nil
}

func (r *mockTemplateRepo) Create(_ *template.Template) error                     { return nil }
func (r *mockTemplateRepo) Update(_ string, _ *template.Template) error           { return nil }
func (r *mockTemplateRepo) Delete(_ string) error                                 { return nil }
func (r *mockTemplateRepo) FindByExternalID(_ string) (*template.Template, error) { return nil, nil }
func (r *mockTemplateRepo) FindByExternalIDAndWABA(_, _ string) (*template.Template, error) {
	return nil, nil
}
func (r *mockTemplateRepo) BatchFindByExternalIDs(_ []string) ([]*template.Template, error) {
	return nil, nil
}
func (r *mockTemplateRepo) FindByName(_, _ string) (*template.Template, error) { return nil, nil }
func (r *mockTemplateRepo) FindByNameAndWABA(_, _, _ string) (*template.Template, error) {
	return nil, nil
}
func (r *mockTemplateRepo) List(_ template.ListInput) (*shared.PaginatedResult[*template.Template], error) {
	return nil, nil
}
func (r *mockTemplateRepo) UpdateStatus(_ string, _ template.TemplateStatus) error { return nil }
func (r *mockTemplateRepo) UpdateHeaderMediaURL(_ string, _ *string) error         { return nil }
func (r *mockTemplateRepo) UpdateHeaderMedia(_ string, _ *string, _ *string) error { return nil }
func (r *mockTemplateRepo) SyncFromExternal(_ *template.Template) error            { return nil }

type mockBusinessPhoneRepo struct{}

func (r *mockBusinessPhoneRepo) Create(_ *businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (r *mockBusinessPhoneRepo) Update(_ string, _ *businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (r *mockBusinessPhoneRepo) Delete(_ string) error { return nil }
func (r *mockBusinessPhoneRepo) FindByID(_ string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (r *mockBusinessPhoneRepo) FindByMetaPhoneNumberID(_ string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (r *mockBusinessPhoneRepo) FindByDisplayPhoneNumber(_ string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (r *mockBusinessPhoneRepo) FindByWABAId(_ string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (r *mockBusinessPhoneRepo) List(_ businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	return nil, nil
}
func (r *mockBusinessPhoneRepo) ListAll() ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (r *mockBusinessPhoneRepo) BatchUpdate(_ []*businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (r *mockBusinessPhoneRepo) UpdateStatus(_ string, _ businessphone.Status) error { return nil }
func (r *mockBusinessPhoneRepo) UpdateBusinessProfile(_ string, _ businessphone.BusinessProfile) error {
	return nil
}
func (r *mockBusinessPhoneRepo) SyncFromMeta(_ *businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (r *mockBusinessPhoneRepo) ClearAccessToken(_ string) error { return nil }
func (r *mockBusinessPhoneRepo) ClearOwner(_ string) error { return nil }
func (r *mockBusinessPhoneRepo) FindByMetaPhoneNumberIDUnscoped(_ string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, businessphone.ErrPhoneNumberNotFound
}
func (r *mockBusinessPhoneRepo) Restore(_ string) error { return nil }

type mockWhatsAppClient struct {
	sendCalls atomic.Int32
	failSend  bool

	failWithPayload []byte
	failWithStatus  int
}

func (c *mockWhatsAppClient) SendTemplateMessage(_ context.Context, _ conversation.SendTemplateMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.sendCalls.Add(1)
	if len(c.failWithPayload) > 0 {
		status := c.failWithStatus
		if status == 0 {
			status = 400
		}
		return &conversation.SendTextMessageOutput{
			ResponsePayload: c.failWithPayload,
			ResponseStatus:  status,
		}, fmt.Errorf("whatsapp send template failed: status=%d body=%s", status, string(c.failWithPayload))
	}
	if c.failSend {
		return nil, fmt.Errorf("send failed")
	}
	return &conversation.SendTextMessageOutput{MessageID: "wamid-test-123"}, nil
}

func (c *mockWhatsAppClient) SendTextMessage(_ context.Context, _ conversation.SendTextMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}
func (c *mockWhatsAppClient) SendAudioMessage(_ context.Context, _ conversation.SendAudioMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}
func (c *mockWhatsAppClient) SendAudioBytes(_ context.Context, _ string, _ []byte, _, _ string) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}
func (c *mockWhatsAppClient) UploadAudio(_ context.Context, _ []byte, _ string) (string, error) {
	return "", nil
}
func (c *mockWhatsAppClient) UploadImage(_ context.Context, _ []byte, _, _ string) (string, error) {
	return "", nil
}
func (c *mockWhatsAppClient) UploadMedia(_ context.Context, _ []byte, _, _ string) (string, error) {
	return "", nil
}
func (c *mockWhatsAppClient) DownloadMedia(_ context.Context, _ string) ([]byte, string, error) {
	return nil, "", nil
}
func (c *mockWhatsAppClient) SendImageMessage(_ context.Context, _ conversation.SendImageMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}
func (c *mockWhatsAppClient) SendVideoMessage(_ context.Context, _ conversation.SendVideoMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}
func (c *mockWhatsAppClient) SendDocumentMessage(_ context.Context, _ conversation.SendDocumentMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}
func (c *mockWhatsAppClient) SendStickerMessage(_ context.Context, _ conversation.SendStickerMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}
func (c *mockWhatsAppClient) SendButtonMessage(_ context.Context, _ conversation.SendButtonMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}
func (c *mockWhatsAppClient) SendListMessage(_ context.Context, _ conversation.SendListMessageInput) (*conversation.SendTextMessageOutput, error) {
	return nil, nil
}
func (c *mockWhatsAppClient) SendTypingIndicator(_ context.Context, _ string) error { return nil }
func (c *mockWhatsAppClient) MarkMessageAsRead(_ context.Context, _ string) error   { return nil }
func (c *mockWhatsAppClient) ListTemplates(_ context.Context, _ conversation.ListTemplatesInput) (*conversation.ListTemplatesOutput, error) {
	return nil, nil
}
func (c *mockWhatsAppClient) GetTemplate(_ context.Context, _ string) (*conversation.Template, error) {
	return nil, nil
}
func (c *mockWhatsAppClient) CreateTemplate(_ context.Context, _ conversation.CreateTemplateInput) (*conversation.CreateTemplateOutput, error) {
	return nil, nil
}
func (c *mockWhatsAppClient) UpdateTemplate(_ context.Context, _ string, _ conversation.UpdateTemplateInput) error {
	return nil
}
func (c *mockWhatsAppClient) DeleteTemplate(_ context.Context, _ conversation.DeleteTemplateInput) error {
	return nil
}
func (c *mockWhatsAppClient) UploadMediaForTemplate(_ context.Context, _ conversation.UploadMediaForTemplateInput) (string, error) {
	return "", nil
}

type mockWhatsAppClientFactory struct {
	client     *mockWhatsAppClient
	returnReal bool
}

func (f *mockWhatsAppClientFactory) ClientForPhone(_ string) (conversation.WhatsAppClient, error) {
	if f.returnReal {
		return f.client, nil
	}
	return nil, fmt.Errorf("not a full client — use sendTemplateMessage path")
}

func (f *mockWhatsAppClientFactory) ClientForWABA(_ string) (conversation.WhatsAppClient, error) {
	return nil, nil
}

func (f *mockWhatsAppClientFactory) WABAIdForPhone(_ string) (string, error) {
	return "", nil
}

type mockCheckBalance struct {
	hasFunds   atomic.Bool
	costMicros int64
}

func (m *mockCheckBalance) Execute(_ string, _ int64) (bool, error) {
	if m.hasFunds.Load() {
		return true, nil
	}
	return false, nil
}

type mockConsumeWhatsappTemplate struct {
	mu             sync.Mutex
	consumed       atomic.Int32
	refunded       atomic.Int32
	failDebit      bool
	panicOnExecute bool
	cost           int64
	costErr        error
	executeErr     error
	executeCalls   []string
	costCalls      []string
}

func (m *mockConsumeWhatsappTemplate) Execute(_, _, templateCategory string) (*balance.Transaction, error) {
	if m.panicOnExecute {
		panic("simulated panic in Execute")
	}
	m.mu.Lock()
	m.executeCalls = append(m.executeCalls, templateCategory)
	m.mu.Unlock()
	if m.executeErr != nil {
		return nil, m.executeErr
	}
	if m.failDebit {
		return nil, balance.ErrInsufficientBalance
	}
	m.consumed.Add(1)
	return &balance.Transaction{}, nil
}

func (m *mockConsumeWhatsappTemplate) Refund(_, _, _ string) error {
	m.refunded.Add(1)
	return nil
}

func (m *mockConsumeWhatsappTemplate) GetTemplateCostMicros(_, templateCategory string) (int64, error) {
	m.mu.Lock()
	m.costCalls = append(m.costCalls, templateCategory)
	m.mu.Unlock()
	if m.costErr != nil {
		return 0, m.costErr
	}
	if m.cost > 0 {
		return m.cost, nil
	}
	return 100, nil
}

func (m *mockConsumeWhatsappTemplate) lastExecuteCategory() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.executeCalls) == 0 {
		return ""
	}
	return m.executeCalls[len(m.executeCalls)-1]
}

func (m *mockConsumeWhatsappTemplate) lastCostCategory() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.costCalls) == 0 {
		return ""
	}
	return m.costCalls[len(m.costCalls)-1]
}

type mockCachedBalanceChecker struct {
	mu            sync.Mutex
	balanceMicros int64
	getBalanceErr error
}

func (m *mockCachedBalanceChecker) HasSufficientBalance(_ string, amountMicros int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getBalanceErr != nil {
		return false, m.getBalanceErr
	}
	return m.balanceMicros >= amountMicros, nil
}

func (m *mockCachedBalanceChecker) GetBalance(_ string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getBalanceErr != nil {
		return 0, m.getBalanceErr
	}
	return m.balanceMicros, nil
}

func (m *mockCachedBalanceChecker) Invalidate(_ string)          {}
func (m *mockCachedBalanceChecker) InvalidateDebounced(_ string) {}

type mockInflightReserver struct {
	shared *mockSharedState
}

func (r *mockInflightReserver) Reserve(workspaceID string, deltaMicros int64, budgetMicros int64) (bool, error) {
	return r.shared.TryIncrBy("balance:inflight:"+workspaceID, deltaMicros, budgetMicros)
}

func (r *mockInflightReserver) Release(workspaceID string, deltaMicros int64) error {
	_, err := r.shared.DecrBy("balance:inflight:"+workspaceID, deltaMicros)
	return err
}

func (r *mockInflightReserver) RefreshTTL(_ string, _ time.Duration) error {
	return nil
}

func (r *mockInflightReserver) GetInflight(workspaceID string) (int64, error) {
	val, err := r.shared.GetString("balance:inflight:" + workspaceID)
	if err != nil || val == "" {
		return 0, nil
	}
	return strconv.ParseInt(val, 10, 64)
}

type mockRecordMetric struct{}

func (m *mockRecordMetric) Execute(_ business_metrics.RecordMetricInput) error { return nil }

type mockMessageHistoryManager struct{}

func (m *mockMessageHistoryManager) Record(_ context.Context, _ conversation.MessageHistoryDirection, _ conversation.MessageHistoryRecord) error {
	return nil
}

type mockWorkspaceConfigRepo struct{}

func (m *mockWorkspaceConfigRepo) GetByWorkspaceID(_ context.Context, _ string) (*wsc.WorkspaceConfig, error) {
	return &wsc.WorkspaceConfig{CampaignSpamProtectionDays: 0}, nil
}

func (m *mockWorkspaceConfigRepo) Upsert(_ context.Context, _ *wsc.WorkspaceConfig) error { return nil }
func (m *mockWorkspaceConfigRepo) EnsureExists(_ context.Context, _ string) error         { return nil }

type mockLeadCampaignSendRepo struct{}

func (m *mockLeadCampaignSendRepo) Record(_, _, _ string) error                     { return nil }
func (m *mockLeadCampaignSendRepo) GetLastSendTime(_, _ string) (*time.Time, error) { return nil, nil }
func (m *mockLeadCampaignSendRepo) GetLastSendTimesBatch(_ []string, _ string) (map[string]time.Time, error) {
	return nil, nil
}

type testHarness struct {
	consumer             *messageConsumerUseCase
	queueSub             *mockMessageQueueSub
	queuePub             *mockMessageQueuePub
	shared               *mockSharedState
	campaignRepo         *mockCampaignRepo
	entryRepo            *mockEntryRepo
	templateRepo         *mockTemplateRepo
	checkBalance         *mockCheckBalance
	consumeTempl         *mockConsumeWhatsappTemplate
	waClient             *mockWhatsAppClient
	inflightReserver     *mockInflightReserver
	cachedBalanceChecker *mockCachedBalanceChecker
}

func approvedMarketingTemplate(id string) *template.Template {
	return &template.Template{
		ID:       id,
		Name:     "test-template",
		Status:   template.TemplateStatusApproved,
		Category: template.TemplateCategoryMarketing,
	}
}

func newTestHarness() *testHarness {
	queueSub := newMockQueueSub()
	queuePub := newMockQueuePub()
	sharedState := newMockSharedState()
	campaignRepo := newMockCampaignRepo()
	entryRepo := newMockEntryRepo()
	templateRepo := newMockTemplateRepo()
	templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")
	checkBal := &mockCheckBalance{}
	checkBal.hasFunds.Store(true)
	consumeTempl := &mockConsumeWhatsappTemplate{}
	waClient := &mockWhatsAppClient{}
	inflightReserver := &mockInflightReserver{shared: sharedState}
	cachedBal := &mockCachedBalanceChecker{balanceMicros: 1_000_000}

	uc := &messageConsumerUseCase{
		MessageQueueSub:         queueSub,
		MessageQueuePub:         queuePub,
		CampaignRepo:            campaignRepo,
		EntryRepo:               entryRepo,
		TemplateRepo:            templateRepo,
		BusinessPhoneRepo:       &mockBusinessPhoneRepo{},
		WhatsAppClientFactory:   &mockWhatsAppClientFactory{client: waClient},
		RecordMetric:            &mockRecordMetric{},
		ConsumeWhatsappTemplate: consumeTempl,
		CheckBalance:            checkBal,
		MessageHistoryManager:   &mockMessageHistoryManager{},
		shared:                  sharedState,
		WorkspaceConfigRepo:     &mockWorkspaceConfigRepo{},
		LeadCampaignSendRepo:    &mockLeadCampaignSendRepo{},
		InflightReserver:        inflightReserver,
		CachedBalanceChecker:    cachedBal,
		subscribedCamps:         make(map[string]bool),
	}

	return &testHarness{
		consumer:             uc,
		queueSub:             queueSub,
		queuePub:             queuePub,
		shared:               sharedState,
		campaignRepo:         campaignRepo,
		entryRepo:            entryRepo,
		templateRepo:         templateRepo,
		checkBalance:         checkBal,
		consumeTempl:         consumeTempl,
		waClient:             waClient,
		inflightReserver:     inflightReserver,
		cachedBalanceChecker: cachedBal,
	}
}

func (h *testHarness) seedCampaign(campID string) {
	tmplID := "tmpl-" + campID
	h.templateRepo.mu.Lock()
	h.templateRepo.templates[tmplID] = approvedMarketingTemplate(tmplID)
	h.templateRepo.mu.Unlock()

	h.campaignRepo.Create(&wc.Campaign{
		ID:              campID,
		Status:          wc.CampaignStatusRunning,
		WorkspaceID:     "ws-1",
		BusinessPhoneID: "bp-1",
		TemplateID:      tmplID,
	})
}

func makeTopic(campaignID string) string {
	return fmt.Sprintf("%s.%s", wc.WhatsAppCampaignDispatchTopic, campaignID)
}

func makePayload(campaignID, entryID, phone string) []byte {
	msg := wc.DispatchQueueMessage{
		CampaignID:  campaignID,
		EntryID:     entryID,
		PhoneNumber: phone,
	}
	b, _ := json.Marshal(msg)
	return b
}

func TestStart_SubscribesToRunningCampaigns(t *testing.T) {
	h := newTestHarness()

	h.campaignRepo.Create(&wc.Campaign{ID: "camp-1", Status: wc.CampaignStatusRunning, WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1"})
	h.campaignRepo.Create(&wc.Campaign{ID: "camp-2", Status: wc.CampaignStatusRunning, WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1"})

	h.campaignRepo.Create(&wc.Campaign{ID: "camp-stopped", Status: wc.CampaignStatusStopped, WorkspaceID: "ws-1"})

	if err := h.consumer.Start(); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	if !h.consumer.IsSubscribed("camp-1") {
		t.Error("expected camp-1 to be subscribed")
	}
	if !h.consumer.IsSubscribed("camp-2") {
		t.Error("expected camp-2 to be subscribed")
	}
	if h.consumer.IsSubscribed("camp-stopped") {
		t.Error("expected camp-stopped to NOT be subscribed")
	}

	topic1 := makeTopic("camp-1")
	topic2 := makeTopic("camp-2")
	if !h.queueSub.isSubscribed(topic1) {
		t.Error("expected queue subscription for camp-1")
	}
	if !h.queueSub.isSubscribed(topic2) {
		t.Error("expected queue subscription for camp-2")
	}
}

func TestStart_NoQueueSub_ReturnsError(t *testing.T) {
	h := newTestHarness()
	h.consumer.MessageQueueSub = nil

	err := h.consumer.Start()
	if err == nil {
		t.Fatal("expected error when MessageQueueSub is nil")
	}
}

func TestStart_NoWhatsAppClientFactory_SkipsGracefully(t *testing.T) {
	h := newTestHarness()
	h.consumer.WhatsAppClientFactory = nil

	err := h.consumer.Start()
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestSubscribe_RegistersTopicHandler(t *testing.T) {
	h := newTestHarness()
	campID := "camp-sub-test"
	h.seedCampaign(campID)

	if err := h.consumer.SubscribeToCampaign(campID); err != nil {
		t.Fatalf("SubscribeToCampaign() error: %v", err)
	}

	if !h.consumer.IsSubscribed(campID) {
		t.Error("expected IsSubscribed to return true")
	}

	topic := makeTopic(campID)
	if !h.queueSub.isSubscribed(topic) {
		t.Error("expected queue handler registered for topic")
	}
}

func TestSubscribe_Idempotent(t *testing.T) {
	h := newTestHarness()
	campID := "camp-idempotent"
	h.seedCampaign(campID)

	_ = h.consumer.SubscribeToCampaign(campID)
	err := h.consumer.SubscribeToCampaign(campID)
	if err != nil {
		t.Fatalf("second SubscribeToCampaign should succeed: %v", err)
	}
}

func TestSubscribe_UsesLatestTemplateCategoryAtSendTime(t *testing.T) {
	h := newTestHarness()
	campID := "camp-refresh-category"
	h.seedCampaign(campID)
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}

	if err := h.consumer.SubscribeToCampaign(campID); err != nil {
		t.Fatalf("SubscribeToCampaign() error: %v", err)
	}

	tmplID := "tmpl-" + campID
	h.templateRepo.mu.Lock()
	h.templateRepo.templates[tmplID] = &template.Template{
		ID:       tmplID,
		Name:     "test-template",
		Status:   template.TemplateStatusApproved,
		Category: template.TemplateCategoryUtility,
	}
	h.templateRepo.mu.Unlock()

	ack := h.queueSub.deliver(makeTopic(campID), makePayload(campID, "entry-refresh-category", "5511999999999"))
	if ack == nil {
		t.Fatal("expected queue delivery ack")
	}
	if !ack.acked.Load() {
		t.Fatal("expected message to be acked")
	}
	if got := h.consumeTempl.lastCostCategory(); got != "UTILITY" {
		t.Fatalf("expected current template cost category UTILITY, got %s", got)
	}
	if got := h.consumeTempl.lastExecuteCategory(); got != "UTILITY" {
		t.Fatalf("expected current template debit category UTILITY, got %s", got)
	}
}

func TestSubscribe_BlocksSendWhenTemplateBecomesNotReady(t *testing.T) {
	h := newTestHarness()
	campID := "camp-template-paused"
	h.seedCampaign(campID)
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}

	if err := h.consumer.SubscribeToCampaign(campID); err != nil {
		t.Fatalf("SubscribeToCampaign() error: %v", err)
	}

	tmplID := "tmpl-" + campID
	h.templateRepo.mu.Lock()
	h.templateRepo.templates[tmplID] = &template.Template{
		ID:       tmplID,
		Name:     "test-template",
		Status:   template.TemplateStatusPaused,
		Category: template.TemplateCategoryMarketing,
	}
	h.templateRepo.mu.Unlock()

	entryID := "entry-template-paused"
	h.entryRepo.entries[entryID] = &wce.WhatsAppCampaignEntry{ID: entryID}

	ack := h.queueSub.deliver(makeTopic(campID), makePayload(campID, entryID, "5511999999999"))
	if ack == nil {
		t.Fatal("expected queue delivery ack")
	}
	if !ack.acked.Load() {
		t.Fatal("expected message to be acked")
	}
	if h.consumeTempl.consumed.Load() != 0 {
		t.Fatal("template should not be billed once it is no longer ready")
	}
	if h.waClient.sendCalls.Load() != 0 {
		t.Fatal("template should not be sent once it is no longer ready")
	}
	if status := h.entryRepo.getStatus(entryID); status != wce.SendStatusFailed {
		t.Fatalf("expected entry status FAILED, got %s", status)
	}
}

func TestSubscribe_ClearsPauseAndStopFlags(t *testing.T) {
	h := newTestHarness()
	campID := "camp-flags"

	h.shared.SetString("campaign:whatsapp:paused:"+campID, "1", 0)
	h.shared.SetString("campaign:whatsapp:stopped:"+campID, "1", 0)

	_ = h.consumer.SubscribeToCampaign(campID)

	paused, _ := h.shared.Exists("campaign:whatsapp:paused:" + campID)
	stopped, _ := h.shared.Exists("campaign:whatsapp:stopped:" + campID)
	if paused {
		t.Error("expected paused flag to be cleared")
	}
	if stopped {
		t.Error("expected stopped flag to be cleared")
	}
}

func TestStop_DeletesQueueAndSetsFlag(t *testing.T) {
	h := newTestHarness()
	campID := "camp-stop"

	_ = h.consumer.SubscribeToCampaign(campID)

	if err := h.consumer.StopCampaignConsumer(campID); err != nil {
		t.Fatalf("StopCampaignConsumer() error: %v", err)
	}

	topic := makeTopic(campID)
	if !h.queueSub.isDeleted(topic) {
		t.Error("expected queue to be deleted")
	}

	stopped, _ := h.shared.Exists("campaign:whatsapp:stopped:" + campID)
	if !stopped {
		t.Error("expected stopped flag to be set in Redis")
	}

	paused, _ := h.shared.Exists("campaign:whatsapp:paused:" + campID)
	if paused {
		t.Error("expected paused flag to be cleared on stop")
	}

	if h.consumer.IsSubscribed(campID) {
		t.Error("expected campaign to no longer be in subscribedCamps")
	}
}

func TestStop_Idempotent(t *testing.T) {
	h := newTestHarness()
	campID := "camp-stop-twice"

	_ = h.consumer.SubscribeToCampaign(campID)
	_ = h.consumer.StopCampaignConsumer(campID)
	err := h.consumer.StopCampaignConsumer(campID)

	if err != nil {
		t.Fatalf("second stop should not error: %v", err)
	}
}

func TestPause_SetsPausedFlag(t *testing.T) {
	h := newTestHarness()
	campID := "camp-pause"

	if err := h.consumer.PauseCampaignConsumer(campID); err != nil {
		t.Fatalf("PauseCampaignConsumer() error: %v", err)
	}

	paused, _ := h.shared.Exists("campaign:whatsapp:paused:" + campID)
	if !paused {
		t.Error("expected paused flag to be set")
	}
}

func TestResume_ClearsPausedFlag(t *testing.T) {
	h := newTestHarness()
	campID := "camp-resume"

	_ = h.consumer.PauseCampaignConsumer(campID)
	if err := h.consumer.ResumeCampaignConsumer(campID); err != nil {
		t.Fatalf("ResumeCampaignConsumer() error: %v", err)
	}

	paused, _ := h.shared.Exists("campaign:whatsapp:paused:" + campID)
	if paused {
		t.Error("expected paused flag to be cleared after resume")
	}
}

func TestLifecycle_Subscribe_Pause_Resume_Stop(t *testing.T) {
	h := newTestHarness()
	campID := "camp-lifecycle"
	h.seedCampaign(campID)

	_ = h.consumer.SubscribeToCampaign(campID)
	if !h.consumer.IsSubscribed(campID) {
		t.Fatal("should be subscribed")
	}

	_ = h.consumer.PauseCampaignConsumer(campID)
	paused, _ := h.shared.Exists("campaign:whatsapp:paused:" + campID)
	if !paused {
		t.Fatal("should be paused")
	}
	if h.consumer.IsSubscribed(campID) {
		t.Fatal("should stop consuming while paused")
	}

	_ = h.consumer.ResumeCampaignConsumer(campID)
	paused, _ = h.shared.Exists("campaign:whatsapp:paused:" + campID)
	if paused {
		t.Fatal("should not be paused after resume")
	}

	if !h.consumer.IsSubscribed(campID) {
		t.Fatal("should resubscribe after resume")
	}

	_ = h.consumer.StopCampaignConsumer(campID)
	if h.consumer.IsSubscribed(campID) {
		t.Fatal("should not be subscribed after stop")
	}

	stopped, _ := h.shared.Exists("campaign:whatsapp:stopped:" + campID)
	if !stopped {
		t.Fatal("stopped flag should be set")
	}
}

func TestLifecycle_StopWhilePaused(t *testing.T) {
	h := newTestHarness()
	campID := "camp-stop-while-paused"

	_ = h.consumer.SubscribeToCampaign(campID)
	_ = h.consumer.PauseCampaignConsumer(campID)
	_ = h.consumer.StopCampaignConsumer(campID)

	if h.consumer.IsSubscribed(campID) {
		t.Error("should not be subscribed after stop")
	}

	stopped, _ := h.shared.Exists("campaign:whatsapp:stopped:" + campID)
	if !stopped {
		t.Error("stopped flag should be set")
	}

	paused, _ := h.shared.Exists("campaign:whatsapp:paused:" + campID)
	if paused {
		t.Error("paused flag should be cleared on stop")
	}
}

func TestLifecycle_ResubscribeAfterStop(t *testing.T) {
	h := newTestHarness()
	campID := "camp-resub"
	h.seedCampaign(campID)

	_ = h.consumer.SubscribeToCampaign(campID)
	_ = h.consumer.StopCampaignConsumer(campID)

	if err := h.consumer.SubscribeToCampaign(campID); err != nil {
		t.Fatalf("re-subscribe after stop should work: %v", err)
	}

	if !h.consumer.IsSubscribed(campID) {
		t.Error("should be subscribed after re-subscribe")
	}

	stopped, _ := h.shared.Exists("campaign:whatsapp:stopped:" + campID)
	if stopped {
		t.Error("stopped flag should be cleared on re-subscribe")
	}
}

func TestHandler_PausedCampaign_RequeuesWithDelay(t *testing.T) {
	h := newTestHarness()
	campID := "camp-paused-msg"
	topic := makeTopic(campID)

	h.seedCampaign(campID)

	_ = h.consumer.SubscribeToCampaign(campID)
	_ = h.shared.SetString("campaign:whatsapp:paused:"+campID, "1", 0)

	payload := makePayload(campID, "entry-1", "5584999990001")

	ack := h.queueSub.deliver(topic, payload)
	if ack == nil {
		t.Fatal("expected ack to be non-nil")
	}
	if !ack.acked.Load() {
		t.Error("expected message to be acked (delay requeue pattern)")
	}
	if ack.nacked.Load() {
		t.Error("expected message NOT to be nacked")
	}

	delayed := h.queuePub.delayedMessagesFor(topic)
	if len(delayed) != 1 {
		t.Fatalf("expected 1 delayed message, got %d", len(delayed))
	}
	if delayed[0].delay != pauseRequeueDelay {
		t.Errorf("expected delay %v, got %v", pauseRequeueDelay, delayed[0].delay)
	}
}

func TestHandler_InsufficientBalance_RequeuesWithDelay(t *testing.T) {
	h := newTestHarness()
	campID := "camp-no-balance"
	topic := makeTopic(campID)

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")

	_ = h.consumer.SubscribeToCampaign(campID)

	h.cachedBalanceChecker.mu.Lock()
	h.cachedBalanceChecker.balanceMicros = 50
	h.cachedBalanceChecker.mu.Unlock()

	payload := makePayload(campID, "entry-1", "5584999990001")
	ack := h.queueSub.deliver(topic, payload)

	if ack == nil {
		t.Fatal("expected ack to be non-nil")
	}
	if !ack.acked.Load() {
		t.Error("expected message to be acked for delay requeue")
	}

	delayed := h.queuePub.delayedMessagesFor(topic)
	if len(delayed) != 1 {
		t.Fatalf("expected 1 delayed message for balance, got %d", len(delayed))
	}
	if delayed[0].delay != balanceRequeueDelay {
		t.Errorf("expected delay %v, got %v", balanceRequeueDelay, delayed[0].delay)
	}

	if h.consumeTempl.consumed.Load() != 0 {
		t.Error("expected no debit when balance is insufficient")
	}
}

func TestHandler_DebitInsufficientBalance_RequeuesWithDelay(t *testing.T) {
	h := newTestHarness()
	campID := "camp-debit-fail"
	topic := makeTopic(campID)

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")

	_ = h.consumer.SubscribeToCampaign(campID)

	h.consumeTempl.failDebit = true

	payload := makePayload(campID, "entry-1", "5584999990001")
	ack := h.queueSub.deliver(topic, payload)

	if ack == nil {
		t.Fatal("expected ack to be non-nil")
	}
	if !ack.acked.Load() {
		t.Error("expected message to be acked for delay requeue")
	}

	delayed := h.queuePub.delayedMessagesFor(topic)
	if len(delayed) != 1 {
		t.Fatalf("expected 1 delayed message for debit failure, got %d", len(delayed))
	}
	if delayed[0].delay != balanceRequeueDelay {
		t.Errorf("expected delay %v, got %v", balanceRequeueDelay, delayed[0].delay)
	}
}

func TestHandler_StoppedCampaign_DoesNotDelayRequeue(t *testing.T) {
	h := newTestHarness()
	campID := "camp-stopped-no-delay"
	topic := makeTopic(campID)

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})

	_ = h.consumer.SubscribeToCampaign(campID)

	h.shared.SetString("campaign:whatsapp:stopped:"+campID, "1", 0)

	payload := makePayload(campID, "entry-1", "5584999990001")
	ack := h.queueSub.deliver(topic, payload)

	if ack == nil {
		t.Fatal("expected ack to be non-nil")
	}

	delayed := h.queuePub.delayedMessagesFor(topic)
	if len(delayed) != 0 {
		t.Errorf("expected no delayed messages for stopped campaign, got %d", len(delayed))
	}
}

func TestConcurrent_MultipleSubscribesAndStops(t *testing.T) {
	h := newTestHarness()

	const numCampaigns = 50
	var wg sync.WaitGroup
	for i := 0; i < numCampaigns; i++ {
		h.seedCampaign(fmt.Sprintf("camp-concurrent-%d", i))
	}

	for i := 0; i < numCampaigns; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			campID := fmt.Sprintf("camp-concurrent-%d", idx)
			_ = h.consumer.SubscribeToCampaign(campID)
		}(i)
	}
	wg.Wait()

	for i := 0; i < numCampaigns; i++ {
		campID := fmt.Sprintf("camp-concurrent-%d", i)
		if !h.consumer.IsSubscribed(campID) {
			t.Errorf("expected %s to be subscribed", campID)
		}
	}

	for i := 0; i < numCampaigns; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			campID := fmt.Sprintf("camp-concurrent-%d", idx)
			_ = h.consumer.StopCampaignConsumer(campID)
		}(i)
	}
	wg.Wait()

	for i := 0; i < numCampaigns; i++ {
		campID := fmt.Sprintf("camp-concurrent-%d", i)
		if h.consumer.IsSubscribed(campID) {
			t.Errorf("expected %s to be unsubscribed after stop", campID)
		}
	}
}

func TestConcurrent_PauseResumeWhileSubscribing(t *testing.T) {
	h := newTestHarness()
	campID := "camp-race"
	h.seedCampaign(campID)

	_ = h.consumer.SubscribeToCampaign(campID)

	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = h.consumer.PauseCampaignConsumer(campID)
		}()
		go func() {
			defer wg.Done()
			_ = h.consumer.ResumeCampaignConsumer(campID)
		}()
	}
	wg.Wait()

	_ = h.consumer.ResumeCampaignConsumer(campID)

	if !h.consumer.IsSubscribed(campID) {
		t.Error("should be subscribed after final resume")
	}
}

func TestExhaustive_StartManyStopResumeCycle(t *testing.T) {
	h := newTestHarness()

	const numCampaigns = 100

	for i := 0; i < numCampaigns; i++ {
		h.seedCampaign(fmt.Sprintf("exhaust-%d", i))
	}

	if err := h.consumer.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	for i := 0; i < numCampaigns; i++ {
		campID := fmt.Sprintf("exhaust-%d", i)
		if !h.consumer.IsSubscribed(campID) {
			t.Fatalf("expected %s to be subscribed after Start", campID)
		}
	}

	for i := 0; i < numCampaigns; i++ {
		_ = h.consumer.PauseCampaignConsumer(fmt.Sprintf("exhaust-%d", i))
	}

	var wg sync.WaitGroup
	for i := 0; i < numCampaigns; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			campID := fmt.Sprintf("exhaust-%d", idx)
			if idx%2 == 0 {
				_ = h.consumer.ResumeCampaignConsumer(campID)
			} else {
				_ = h.consumer.StopCampaignConsumer(campID)
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < numCampaigns; i++ {
		campID := fmt.Sprintf("exhaust-%d", i)
		if i%2 == 0 {
			if !h.consumer.IsSubscribed(campID) {
				t.Errorf("expected %s (even) to still be subscribed", campID)
			}

			paused, _ := h.shared.Exists("campaign:whatsapp:paused:" + campID)
			if paused {
				t.Errorf("expected %s to not be paused after resume", campID)
			}
		} else {
			if h.consumer.IsSubscribed(campID) {
				t.Errorf("expected %s (odd) to NOT be subscribed", campID)
			}
			stopped, _ := h.shared.Exists("campaign:whatsapp:stopped:" + campID)
			if !stopped {
				t.Errorf("expected %s stopped flag to be set", campID)
			}
		}
	}
}

func TestExhaustive_ResubscribeAll(t *testing.T) {
	h := newTestHarness()

	const numCampaigns = 50

	for i := 0; i < numCampaigns; i++ {
		campID := fmt.Sprintf("resub-%d", i)
		h.seedCampaign(campID)
		_ = h.consumer.SubscribeToCampaign(campID)
	}
	for i := 0; i < numCampaigns; i++ {
		campID := fmt.Sprintf("resub-%d", i)
		_ = h.consumer.StopCampaignConsumer(campID)
	}

	for i := 0; i < numCampaigns; i++ {
		campID := fmt.Sprintf("resub-%d", i)
		if h.consumer.IsSubscribed(campID) {
			t.Fatalf("expected %s to be unsubscribed", campID)
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < numCampaigns; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			campID := fmt.Sprintf("resub-%d", idx)
			_ = h.consumer.SubscribeToCampaign(campID)
		}(i)
	}
	wg.Wait()

	for i := 0; i < numCampaigns; i++ {
		campID := fmt.Sprintf("resub-%d", i)
		if !h.consumer.IsSubscribed(campID) {
			t.Errorf("expected %s to be re-subscribed", campID)
		}

		stopped, _ := h.shared.Exists("campaign:whatsapp:stopped:" + campID)
		if stopped {
			t.Errorf("expected stopped flag cleared for %s", campID)
		}
	}
}

func TestExhaustive_RapidPauseStopSequence(t *testing.T) {
	h := newTestHarness()
	campID := "rapid-cycle"
	h.seedCampaign(campID)

	for cycle := 0; cycle < 20; cycle++ {
		_ = h.consumer.SubscribeToCampaign(campID)
		if !h.consumer.IsSubscribed(campID) {
			t.Fatalf("cycle %d: should be subscribed", cycle)
		}

		_ = h.consumer.PauseCampaignConsumer(campID)
		paused, _ := h.shared.Exists("campaign:whatsapp:paused:" + campID)
		if !paused {
			t.Fatalf("cycle %d: should be paused", cycle)
		}

		_ = h.consumer.ResumeCampaignConsumer(campID)
		paused, _ = h.shared.Exists("campaign:whatsapp:paused:" + campID)
		if paused {
			t.Fatalf("cycle %d: should not be paused after resume", cycle)
		}

		_ = h.consumer.StopCampaignConsumer(campID)
		if h.consumer.IsSubscribed(campID) {
			t.Fatalf("cycle %d: should not be subscribed after stop", cycle)
		}
	}
}

var (
	_ cache.SharedState
	_ lead_campaign_send.Repository
)

func TestInflight_ReserveAndRelease_OnSuccessfulSend(t *testing.T) {
	h := newTestHarness()
	campID := "camp-inflight-ok"
	topic := makeTopic(campID)

	h.cachedBalanceChecker.balanceMicros = 10_000
	h.consumeTempl.cost = 500

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")

	_ = h.consumer.SubscribeToCampaign(campID)

	payload := makePayload(campID, "entry-1", "5584999990001")
	ack := h.queueSub.deliver(topic, payload)

	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected message to be acked")
	}
	if h.consumeTempl.consumed.Load() != 1 {
		t.Error("expected 1 debit")
	}

	inflight, _ := h.inflightReserver.GetInflight("ws-1")
	if inflight != 0 {
		t.Fatalf("expected 0 inflight after debit+release, got %d", inflight)
	}
}

func TestInflight_ReserveAndRelease_OnDebitFailure(t *testing.T) {
	h := newTestHarness()
	campID := "camp-inflight-debit-fail"
	topic := makeTopic(campID)

	h.cachedBalanceChecker.balanceMicros = 10_000
	h.consumeTempl.cost = 500
	h.consumeTempl.failDebit = true

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")

	_ = h.consumer.SubscribeToCampaign(campID)

	payload := makePayload(campID, "entry-1", "5584999990001")
	_ = h.queueSub.deliver(topic, payload)

	inflight, _ := h.inflightReserver.GetInflight("ws-1")
	if inflight != 0 {
		t.Fatalf("expected 0 inflight after failed debit, got %d", inflight)
	}
}

func TestInflight_InsufficientBudget_NoReservation(t *testing.T) {
	h := newTestHarness()
	campID := "camp-inflight-nsf"
	topic := makeTopic(campID)

	h.cachedBalanceChecker.balanceMicros = 50
	h.consumeTempl.cost = 100

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")

	_ = h.consumer.SubscribeToCampaign(campID)
	payload := makePayload(campID, "entry-1", "5584999990001")
	ack := h.queueSub.deliver(topic, payload)

	if !ack.acked.Load() {
		t.Error("expected ack for requeue")
	}
	delayed := h.queuePub.delayedMessagesFor(topic)
	if len(delayed) != 1 {
		t.Fatalf("expected 1 requeue, got %d", len(delayed))
	}

	inflight, _ := h.inflightReserver.GetInflight("ws-1")
	if inflight != 0 {
		t.Fatalf("expected 0 inflight, got %d", inflight)
	}
	if h.consumeTempl.consumed.Load() != 0 {
		t.Error("expected no debit")
	}
}

func TestInflight_OtherServiceReservedBudget_BlocksCampaign(t *testing.T) {

	h := newTestHarness()
	campID := "camp-inflight-blocked"
	topic := makeTopic(campID)

	h.cachedBalanceChecker.balanceMicros = 10_000
	h.consumeTempl.cost = 500

	h.shared.TryIncrBy("balance:inflight:ws-1", 9_900, 10_000)

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")

	_ = h.consumer.SubscribeToCampaign(campID)
	payload := makePayload(campID, "entry-1", "5584999990001")
	ack := h.queueSub.deliver(topic, payload)

	if !ack.acked.Load() {
		t.Error("expected ack for requeue")
	}
	delayed := h.queuePub.delayedMessagesFor(topic)
	if len(delayed) != 1 {
		t.Fatalf("expected 1 requeue (budget exceeded by other service), got %d", len(delayed))
	}

	inflight, _ := h.inflightReserver.GetInflight("ws-1")
	if inflight != 9_900 {
		t.Fatalf("expected 9900 inflight (call only), got %d", inflight)
	}
	if h.consumeTempl.consumed.Load() != 0 {
		t.Error("expected no debit when reservation fails")
	}
}

func TestInflight_OtherServiceReservedBudget_StillFitsOneCampaign(t *testing.T) {

	h := newTestHarness()
	campID := "camp-inflight-fits"
	topic := makeTopic(campID)

	h.cachedBalanceChecker.balanceMicros = 10_000
	h.consumeTempl.cost = 500

	h.shared.TryIncrBy("balance:inflight:ws-1", 9_000, 10_000)

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")

	_ = h.consumer.SubscribeToCampaign(campID)
	payload := makePayload(campID, "entry-1", "5584999990001")
	ack := h.queueSub.deliver(topic, payload)

	if !ack.acked.Load() {
		t.Fatal("expected ack on success")
	}
	if h.consumeTempl.consumed.Load() != 1 {
		t.Error("expected 1 debit — budget should have fit the campaign message")
	}

	inflight, _ := h.inflightReserver.GetInflight("ws-1")
	if inflight != 9_000 {
		t.Fatalf("expected 9000 inflight (call reservation only), got %d", inflight)
	}
}

func TestInflight_MultipleCampaignMessages_AtomicReservation(t *testing.T) {

	h := newTestHarness()
	campID := "camp-inflight-multi"
	topic := makeTopic(campID)

	h.cachedBalanceChecker.balanceMicros = 1000
	h.consumeTempl.cost = 500

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-multi", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")
	_ = h.consumer.SubscribeToCampaign(campID)

	p1 := makePayload(campID, "entry-1", "5584999990001")
	ack1 := h.queueSub.deliver(topic, p1)
	if !ack1.acked.Load() {
		t.Fatal("msg1: expected ack")
	}

	p2 := makePayload(campID, "entry-2", "5584999990002")
	ack2 := h.queueSub.deliver(topic, p2)
	if !ack2.acked.Load() {
		t.Fatal("msg2: expected ack")
	}

	if h.consumeTempl.consumed.Load() != 2 {
		t.Fatalf("expected 2 debits, got %d", h.consumeTempl.consumed.Load())
	}
	inflight, _ := h.inflightReserver.GetInflight("ws-multi")
	if inflight != 0 {
		t.Fatalf("expected 0 inflight after all releases, got %d", inflight)
	}
}

func TestInflight_BudgetExactlyEquals_ReservationSucceeds(t *testing.T) {

	h := newTestHarness()
	campID := "camp-exact"
	topic := makeTopic(campID)

	h.cachedBalanceChecker.balanceMicros = 100
	h.consumeTempl.cost = 100

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-exact", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")
	_ = h.consumer.SubscribeToCampaign(campID)

	payload := makePayload(campID, "entry-1", "5584999990001")
	ack := h.queueSub.deliver(topic, payload)

	if !ack.acked.Load() {
		t.Fatal("expected ack on exact-budget match")
	}
	if h.consumeTempl.consumed.Load() != 1 {
		t.Error("expected 1 debit")
	}
}

func TestInflight_NilReserver_FailsClosed(t *testing.T) {
	h := newTestHarness()
	campID := "camp-nil-reserver"
	topic := makeTopic(campID)

	h.consumer.InflightReserver = nil

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")
	_ = h.consumer.SubscribeToCampaign(campID)

	payload := makePayload(campID, "entry-1", "5584999990001")
	ack := h.queueSub.deliver(topic, payload)

	if !ack.nacked.Load() {
		t.Error("expected nack when inflightReserver is nil (fail-closed)")
	}
	if h.consumeTempl.consumed.Load() != 0 {
		t.Error("expected no debit when inflightReserver is nil")
	}
}

func TestInflight_NilCachedBalanceChecker_FailsClosed(t *testing.T) {
	h := newTestHarness()
	campID := "camp-nil-checker"
	topic := makeTopic(campID)

	h.consumer.CachedBalanceChecker = nil

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")
	_ = h.consumer.SubscribeToCampaign(campID)

	payload := makePayload(campID, "entry-1", "5584999990001")
	ack := h.queueSub.deliver(topic, payload)

	if !ack.nacked.Load() {
		t.Error("expected nack when cachedBalanceChecker is nil (fail-closed)")
	}
}

func TestInflight_GetBalanceError_FailsClosed(t *testing.T) {
	h := newTestHarness()
	campID := "camp-getbal-err"
	topic := makeTopic(campID)

	h.cachedBalanceChecker.getBalanceErr = fmt.Errorf("redis down")

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")
	_ = h.consumer.SubscribeToCampaign(campID)

	payload := makePayload(campID, "entry-1", "5584999990001")
	ack := h.queueSub.deliver(topic, payload)

	if !ack.nacked.Load() {
		t.Error("expected nack on GetBalance error (fail-closed)")
	}
	if h.consumeTempl.consumed.Load() != 0 {
		t.Error("expected no debit")
	}
}

func TestInflight_ConfigErrorRefund_InflightAlreadyReleased(t *testing.T) {

	h := newTestHarness()
	campID := "camp-config-err"
	topic := makeTopic(campID)

	h.cachedBalanceChecker.balanceMicros = 10_000
	h.consumeTempl.cost = 500

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})

	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")
	h.waClient.failSend = true

	_ = h.consumer.SubscribeToCampaign(campID)
	payload := makePayload(campID, "entry-1", "5584999990001")
	_ = h.queueSub.deliver(topic, payload)

	if h.consumeTempl.consumed.Load() != 1 {
		t.Fatalf("expected 1 debit, got %d", h.consumeTempl.consumed.Load())
	}

	inflight, _ := h.inflightReserver.GetInflight("ws-1")
	if inflight != 0 {
		t.Fatalf("expected 0 inflight, got %d", inflight)
	}
}

func TestInflight_PanicDuringDebit_DeferReleasesInflight(t *testing.T) {

	h := newTestHarness()
	campID := "camp-panic-debit"
	topic := makeTopic(campID)

	h.cachedBalanceChecker.balanceMicros = 10_000
	h.consumeTempl.cost = 500
	h.consumeTempl.panicOnExecute = true

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-panic", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")

	_ = h.consumer.SubscribeToCampaign(campID)
	payload := makePayload(campID, "entry-1", "5584999990001")

	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		h.queueSub.deliver(topic, payload)
	}()

	if !panicked {
		t.Fatal("expected handler to panic")
	}

	inflight, _ := h.inflightReserver.GetInflight("ws-panic")
	if inflight != 0 {
		t.Fatalf("expected 0 inflight after panic, got %d — defer Release did not fire", inflight)
	}
}

func TestInflight_PanicAfterDebit_DeferReleasesInflight(t *testing.T) {

	h := newTestHarness()
	campID := "camp-panic-after-debit"
	topic := makeTopic(campID)

	h.cachedBalanceChecker.balanceMicros = 10_000
	h.consumeTempl.cost = 500

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-panic2", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")

	h.consumer.WhatsAppClientFactory = &panicWhatsAppClientFactory{}

	_ = h.consumer.SubscribeToCampaign(campID)
	payload := makePayload(campID, "entry-1", "5584999990001")

	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		h.queueSub.deliver(topic, payload)
	}()

	if !panicked {
		t.Fatal("expected handler to panic after debit during send")
	}

	if h.consumeTempl.consumed.Load() != 1 {
		t.Fatalf("expected 1 debit, got %d", h.consumeTempl.consumed.Load())
	}

	inflight, _ := h.inflightReserver.GetInflight("ws-panic2")
	if inflight != 0 {
		t.Fatalf("expected 0 inflight after panic (post-debit), got %d", inflight)
	}
}

type panicWhatsAppClientFactory struct{}

func (f *panicWhatsAppClientFactory) ClientForPhone(_ string) (conversation.WhatsAppClient, error) {
	panic("simulated panic in ClientForPhone")
}

func (f *panicWhatsAppClientFactory) ClientForWABA(_ string) (conversation.WhatsAppClient, error) {
	return nil, nil
}

func (f *panicWhatsAppClientFactory) WABAIdForPhone(_ string) (string, error) {
	return "", nil
}

func setupCampaignWithCounter(h *testHarness, campID string, entryCount int) string {
	topic := makeTopic(campID)

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = &template.Template{
		ID:       "tmpl-1",
		Name:     "test-template",
		Status:   template.TemplateStatusApproved,
		Category: "MARKETING",
	}

	h.shared.SetString(
		"campaign:whatsapp:remaining:"+campID,
		strconv.Itoa(entryCount),
		0,
	)

	h.queueSub.mu.Lock()
	h.queueSub.queueLengths[topic] = 0
	h.queueSub.queueLengths[topic+".delay"] = 0
	h.queueSub.mu.Unlock()

	_ = h.consumer.SubscribeToCampaign(campID)
	return topic
}

func TestCompletion_SingleEntry_CompletesAfterProcessing(t *testing.T) {
	h := newTestHarness()
	campID := "camp-complete-1"
	topic := setupCampaignWithCounter(h, campID, 1)

	ack := h.queueSub.deliver(topic, makePayload(campID, "entry-1", "5584999990001"))
	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected ack")
	}

	camp, _ := h.campaignRepo.FindByID(campID)
	if camp.Status != wc.CampaignStatusCompleted {
		t.Fatalf("expected COMPLETED status, got %s", camp.Status)
	}
	if h.consumer.IsSubscribed(campID) {
		t.Error("expected campaign to be unsubscribed after completion")
	}
}

func TestCompletion_FiveEntries_CompletesOnlyAfterLast(t *testing.T) {
	h := newTestHarness()
	campID := "camp-complete-5"
	topic := setupCampaignWithCounter(h, campID, 5)

	for i := 1; i <= 5; i++ {
		entryID := fmt.Sprintf("entry-%d", i)
		phone := fmt.Sprintf("558499999%04d", i)
		ack := h.queueSub.deliver(topic, makePayload(campID, entryID, phone))
		if ack == nil || !ack.acked.Load() {
			t.Fatalf("entry %d: expected ack", i)
		}

		camp, _ := h.campaignRepo.FindByID(campID)
		if i < 5 {
			if camp.Status != wc.CampaignStatusRunning {
				t.Fatalf("entry %d: expected RUNNING, got %s (premature completion!)", i, camp.Status)
			}
			if !h.consumer.IsSubscribed(campID) {
				t.Fatalf("entry %d: expected still subscribed", i)
			}
		} else {
			if camp.Status != wc.CampaignStatusCompleted {
				t.Fatalf("entry %d: expected COMPLETED after last entry, got %s", i, camp.Status)
			}
		}
	}

	if h.consumeTempl.consumed.Load() != 5 {
		t.Fatalf("expected 5 debits, got %d", h.consumeTempl.consumed.Load())
	}
}

func TestCompletion_NEntries_CompletesOnlyAfterLast(t *testing.T) {
	const N = 50
	h := newTestHarness()
	campID := "camp-complete-N"
	topic := setupCampaignWithCounter(h, campID, N)

	for i := 1; i <= N; i++ {
		entryID := fmt.Sprintf("entry-%d", i)
		phone := fmt.Sprintf("558499900%04d", i)
		ack := h.queueSub.deliver(topic, makePayload(campID, entryID, phone))
		if ack == nil || !ack.acked.Load() {
			t.Fatalf("entry %d: expected ack", i)
		}
	}

	camp, _ := h.campaignRepo.FindByID(campID)
	if camp.Status != wc.CampaignStatusCompleted {
		t.Fatalf("expected COMPLETED after %d entries, got %s", N, camp.Status)
	}
	if h.consumer.IsSubscribed(campID) {
		t.Error("expected unsubscribed after completion")
	}
	if h.consumeTempl.consumed.Load() != N {
		t.Fatalf("expected %d debits, got %d", N, h.consumeTempl.consumed.Load())
	}
}

func TestCompletion_NoCounter_NeverCompletes(t *testing.T) {

	h := newTestHarness()
	campID := "camp-no-counter"
	topic := makeTopic(campID)

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")

	h.queueSub.mu.Lock()
	h.queueSub.queueLengths[topic] = 0
	h.queueSub.queueLengths[topic+".delay"] = 0
	h.queueSub.mu.Unlock()

	_ = h.consumer.SubscribeToCampaign(campID)

	ack := h.queueSub.deliver(topic, makePayload(campID, "entry-1", "5584999990001"))
	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected ack")
	}

	camp, _ := h.campaignRepo.FindByID(campID)
	if camp.Status != wc.CampaignStatusRunning {
		t.Fatalf("expected RUNNING (no phantom completion), got %s", camp.Status)
	}
}

func TestCompletion_QueueReportsZero_OldBehaviorWouldPrematurelyComplete(t *testing.T) {

	h := newTestHarness()
	campID := "camp-regression"
	topic := makeTopic(campID)

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")

	h.shared.SetString("campaign:whatsapp:remaining:"+campID, "5", 0)

	h.queueSub.mu.Lock()
	h.queueSub.queueLengths[topic] = 0
	h.queueSub.queueLengths[topic+".delay"] = 0
	h.queueSub.mu.Unlock()

	_ = h.consumer.SubscribeToCampaign(campID)

	ack := h.queueSub.deliver(topic, makePayload(campID, "entry-1", "5584999990001"))
	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected ack")
	}

	camp, _ := h.campaignRepo.FindByID(campID)
	if camp.Status != wc.CampaignStatusRunning {
		t.Fatalf("REGRESSION: campaign prematurely completed after 1 of 5 entries (status: %s)", camp.Status)
	}

	remaining, _ := h.shared.GetString("campaign:whatsapp:remaining:" + campID)
	if remaining != "4" {
		t.Fatalf("expected remaining=4, got %s", remaining)
	}
}

func TestCounter_PausedMessage_DoesNotDecrementCounter(t *testing.T) {

	h := newTestHarness()
	campID := "camp-pause-counter"
	topic := setupCampaignWithCounter(h, campID, 5)

	for i := 1; i <= 2; i++ {
		ack := h.queueSub.deliver(topic, makePayload(campID, fmt.Sprintf("entry-%d", i), fmt.Sprintf("558499900%04d", i)))
		if ack == nil || !ack.acked.Load() {
			t.Fatalf("entry %d: expected ack", i)
		}
	}

	remaining, _ := h.shared.GetString("campaign:whatsapp:remaining:" + campID)
	if remaining != "3" {
		t.Fatalf("expected remaining=3 after 2 entries, got %s", remaining)
	}

	_ = h.consumer.PauseCampaignConsumer(campID)

	ack := h.queueSub.deliver(topic, makePayload(campID, "entry-3", "5584999000003"))
	if ack != nil {
		t.Fatal("expected no delivery while paused")
	}

	remaining, _ = h.shared.GetString("campaign:whatsapp:remaining:" + campID)
	if remaining != "3" {
		t.Fatalf("expected remaining=3 after paused message, got %s (counter was decremented during pause!)", remaining)
	}

	camp, _ := h.campaignRepo.FindByID(campID)
	if camp.Status != wc.CampaignStatusRunning {
		t.Fatalf("expected RUNNING during pause, got %s", camp.Status)
	}

	delayed := h.queuePub.delayedMessagesFor(topic)
	if len(delayed) != 0 {
		t.Fatalf("expected 0 delayed messages while paused, got %d", len(delayed))
	}
}

func TestCounter_ResumeAfterPause_CompletesAll(t *testing.T) {

	h := newTestHarness()
	campID := "camp-resume-counter"
	topic := setupCampaignWithCounter(h, campID, 5)

	for i := 1; i <= 2; i++ {
		h.queueSub.deliver(topic, makePayload(campID, fmt.Sprintf("entry-%d", i), fmt.Sprintf("558499900%04d", i)))
	}

	_ = h.consumer.PauseCampaignConsumer(campID)

	if ack := h.queueSub.deliver(topic, makePayload(campID, "entry-3", "5584999000003")); ack != nil {
		t.Fatal("expected no delivery while paused")
	}

	remaining, _ := h.shared.GetString("campaign:whatsapp:remaining:" + campID)
	if remaining != "3" {
		t.Fatalf("expected remaining=3 during pause, got %s", remaining)
	}

	_ = h.consumer.ResumeCampaignConsumer(campID)

	for i := 3; i <= 5; i++ {
		ack := h.queueSub.deliver(topic, makePayload(campID, fmt.Sprintf("entry-%d", i), fmt.Sprintf("558499900%04d", i)))
		if ack == nil || !ack.acked.Load() {
			t.Fatalf("entry %d: expected ack after resume", i)
		}
	}

	camp, _ := h.campaignRepo.FindByID(campID)
	if camp.Status != wc.CampaignStatusCompleted {
		t.Fatalf("expected COMPLETED after resume, got %s", camp.Status)
	}
}

func TestCounter_StopMidCampaign_CleansUpCounter(t *testing.T) {

	h := newTestHarness()
	campID := "camp-stop-counter"
	topic := setupCampaignWithCounter(h, campID, 5)

	for i := 1; i <= 2; i++ {
		h.queueSub.deliver(topic, makePayload(campID, fmt.Sprintf("entry-%d", i), fmt.Sprintf("558499900%04d", i)))
	}

	remaining, _ := h.shared.GetString("campaign:whatsapp:remaining:" + campID)
	if remaining != "3" {
		t.Fatalf("expected remaining=3, got %s", remaining)
	}

	_ = h.consumer.StopCampaignConsumer(campID)

	exists, _ := h.shared.Exists("campaign:whatsapp:remaining:" + campID)
	if exists {
		t.Fatal("expected counter key to be deleted after stop")
	}

	if h.consumer.IsSubscribed(campID) {
		t.Error("expected campaign to be unsubscribed after stop")
	}
}

func TestCounter_StopThenRestart_NewCounterAndCompletes(t *testing.T) {

	h := newTestHarness()
	campID := "camp-restart-counter"
	topic := setupCampaignWithCounter(h, campID, 5)

	for i := 1; i <= 2; i++ {
		h.queueSub.deliver(topic, makePayload(campID, fmt.Sprintf("entry-%d", i), fmt.Sprintf("558499900%04d", i)))
	}

	_ = h.consumer.StopCampaignConsumer(campID)

	h.shared.SetString("campaign:whatsapp:remaining:"+campID, "3", 0)

	h.campaignRepo.UpdateStatus(campID, wc.CampaignStatusRunning)

	_ = h.consumer.SubscribeToCampaign(campID)

	for i := 3; i <= 5; i++ {
		ack := h.queueSub.deliver(topic, makePayload(campID, fmt.Sprintf("entry-%d", i), fmt.Sprintf("558499900%04d", i)))
		if ack == nil || !ack.acked.Load() {
			t.Fatalf("entry %d: expected ack after restart", i)
		}
	}

	camp, _ := h.campaignRepo.FindByID(campID)
	if camp.Status != wc.CampaignStatusCompleted {
		t.Fatalf("expected COMPLETED after restart, got %s", camp.Status)
	}
}

func TestCounter_InsufficientBalance_DoesNotDecrementCounter(t *testing.T) {

	h := newTestHarness()
	campID := "camp-balance-counter"
	topic := setupCampaignWithCounter(h, campID, 5)

	for i := 1; i <= 2; i++ {
		h.queueSub.deliver(topic, makePayload(campID, fmt.Sprintf("entry-%d", i), fmt.Sprintf("558499900%04d", i)))
	}

	remaining, _ := h.shared.GetString("campaign:whatsapp:remaining:" + campID)
	if remaining != "3" {
		t.Fatalf("expected remaining=3, got %s", remaining)
	}

	h.cachedBalanceChecker.mu.Lock()
	h.cachedBalanceChecker.balanceMicros = 0
	h.cachedBalanceChecker.mu.Unlock()

	ack := h.queueSub.deliver(topic, makePayload(campID, "entry-3", "5584999000003"))
	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected ack for balance requeue")
	}

	remaining, _ = h.shared.GetString("campaign:whatsapp:remaining:" + campID)
	if remaining != "3" {
		t.Fatalf("expected remaining=3 after insufficient balance, got %s", remaining)
	}

	camp, _ := h.campaignRepo.FindByID(campID)
	if camp.Status != wc.CampaignStatusRunning {
		t.Fatalf("expected RUNNING, got %s", camp.Status)
	}
}

func TestCounter_DebitFail_DoesNotDecrementCounter(t *testing.T) {

	h := newTestHarness()
	campID := "camp-debit-counter"
	topic := setupCampaignWithCounter(h, campID, 5)

	for i := 1; i <= 2; i++ {
		h.queueSub.deliver(topic, makePayload(campID, fmt.Sprintf("entry-%d", i), fmt.Sprintf("558499900%04d", i)))
	}

	h.consumeTempl.failDebit = true

	ack := h.queueSub.deliver(topic, makePayload(campID, "entry-3", "5584999000003"))
	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected ack for debit requeue")
	}

	remaining, _ := h.shared.GetString("campaign:whatsapp:remaining:" + campID)
	if remaining != "3" {
		t.Fatalf("expected remaining=3 after debit fail, got %s", remaining)
	}
}

func TestCounter_StopDuringProcessing_PostDecrDoesNotPhantomComplete(t *testing.T) {

	h := newTestHarness()
	campID := "camp-stop-race"

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")

	h.shared.SetString("campaign:whatsapp:remaining:"+campID, "1", 0)

	h.queueSub.mu.Lock()
	topic := makeTopic(campID)
	h.queueSub.queueLengths[topic] = 0
	h.queueSub.queueLengths[topic+".delay"] = 0
	h.queueSub.mu.Unlock()

	_ = h.consumer.SubscribeToCampaign(campID)

	_ = h.shared.Del("campaign:whatsapp:remaining:" + campID)

	ack := h.queueSub.deliver(topic, makePayload(campID, "entry-1", "5584999990001"))
	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected ack")
	}

	camp, _ := h.campaignRepo.FindByID(campID)
	if camp.Status == wc.CampaignStatusCompleted {
		t.Fatal("PHANTOM COMPLETION: campaign should NOT be completed when counter key is missing")
	}
}

func metaAPIError(code int, message, details string) []byte {
	payload := map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    "OAuthException",
			"code":    code,
			"error_data": map[string]interface{}{
				"messaging_product": "whatsapp",
				"details":           details,
			},
			"fbtrace_id": "Az8or2yhqkZfEZ-_4Qn_Bam",
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

func setupMetaAPIErrorTest(t *testing.T, campID string, errorPayload []byte, httpStatus int) (*testHarness, string) {
	t.Helper()
	h := newTestHarness()

	h.waClient.failWithPayload = errorPayload
	h.waClient.failWithStatus = httpStatus
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}

	topic := makeTopic(campID)

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = &template.Template{
		ID:       "tmpl-1",
		Name:     "promo_template",
		Status:   template.TemplateStatusApproved,
		Category: "MARKETING",
	}

	h.shared.SetString("campaign:whatsapp:remaining:"+campID, "1", 0)
	h.queueSub.mu.Lock()
	h.queueSub.queueLengths[topic] = 0
	h.queueSub.queueLengths[topic+".delay"] = 0
	h.queueSub.mu.Unlock()

	_ = h.consumer.SubscribeToCampaign(campID)
	return h, topic
}

func TestMetaAPI_PaymentNotEligible_RefundsAndSetsError(t *testing.T) {

	campID := "camp-meta-131042"
	payload := metaAPIError(131042,
		"(#131042) Business eligibility payment issue",
		"Payment account is not attached to a WhatsApp Business Account",
	)

	h, topic := setupMetaAPIErrorTest(t, campID, payload, 400)

	entryID := "entry-payment"
	ack := h.queueSub.deliver(topic, makePayload(campID, entryID, "5584999990001"))
	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected message to be acked")
	}

	if h.consumeTempl.consumed.Load() != 1 {
		t.Fatalf("expected 1 debit, got %d", h.consumeTempl.consumed.Load())
	}
	if h.consumeTempl.refunded.Load() != 1 {
		t.Fatalf("expected 1 refund, got %d", h.consumeTempl.refunded.Load())
	}

	if status := h.entryRepo.getStatus(entryID); status != wce.SendStatusFailed {
		t.Fatalf("expected FAILED status, got %s", status)
	}
	if code := h.entryRepo.getErrorCode(entryID); code != 131042 {
		t.Fatalf("expected error code 131042, got %d", code)
	}
	if msg := h.entryRepo.getErrorMessage(entryID); msg == "" {
		t.Fatal("expected non-empty error message on entry")
	}
}

func TestMetaAPI_SpamRateLimit_RefundsAndSetsError(t *testing.T) {

	campID := "camp-meta-131048"
	payload := metaAPIError(131048,
		"(#131048) Spam rate limit hit",
		"Message failed to send because there are restrictions on how many messages can be sent from this phone number.",
	)

	h, topic := setupMetaAPIErrorTest(t, campID, payload, 400)

	entryID := "entry-spam-limit"
	ack := h.queueSub.deliver(topic, makePayload(campID, entryID, "5584999990002"))
	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected ack")
	}

	if h.consumeTempl.refunded.Load() != 1 {
		t.Fatalf("expected 1 refund for spam rate limit, got %d", h.consumeTempl.refunded.Load())
	}
	if code := h.entryRepo.getErrorCode(entryID); code != 131048 {
		t.Fatalf("expected error code 131048, got %d", code)
	}
	if msg := h.entryRepo.getErrorMessage(entryID); msg == "" {
		t.Fatal("expected error message to be set on entry")
	}
}

func TestMetaAPI_MessageUndeliverable_RefundsAndSetsError(t *testing.T) {

	campID := "camp-meta-131026"
	payload := metaAPIError(131026,
		"(#131026) Message Undeliverable",
		"Unable to deliver message. Reasons can include: The recipient phone number is not a WhatsApp phone number.",
	)

	h, topic := setupMetaAPIErrorTest(t, campID, payload, 400)

	entryID := "entry-undeliverable"
	ack := h.queueSub.deliver(topic, makePayload(campID, entryID, "5584999990003"))
	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected ack")
	}

	if h.consumeTempl.refunded.Load() != 1 {
		t.Fatalf("expected 1 refund, got %d", h.consumeTempl.refunded.Load())
	}
	if status := h.entryRepo.getStatus(entryID); status != wce.SendStatusFailed {
		t.Fatalf("expected FAILED, got %s", status)
	}
	if code := h.entryRepo.getErrorCode(entryID); code != 131026 {
		t.Fatalf("expected error code 131026, got %d", code)
	}
}

func TestMetaAPI_RateLimitHit_RefundsAndSetsError(t *testing.T) {

	campID := "camp-meta-130429"
	payload := metaAPIError(130429,
		"(#130429) Rate limit hit",
		"Cloud API message throughput has been reached.",
	)

	h, topic := setupMetaAPIErrorTest(t, campID, payload, 400)

	entryID := "entry-rate-limit"
	ack := h.queueSub.deliver(topic, makePayload(campID, entryID, "5584999990004"))
	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected ack")
	}

	if h.consumeTempl.refunded.Load() != 1 {
		t.Fatalf("expected 1 refund, got %d", h.consumeTempl.refunded.Load())
	}
	if code := h.entryRepo.getErrorCode(entryID); code != 130429 {
		t.Fatalf("expected error code 130429, got %d", code)
	}
	if msg := h.entryRepo.getErrorMessage(entryID); msg == "" {
		t.Fatal("expected error message on entry")
	}
}

func TestMetaAPI_TemplateParamMismatch_RefundsAndSetsError(t *testing.T) {

	campID := "camp-meta-132000"
	payload := metaAPIError(132000,
		"(#132000) Number of parameters does not match the expected number of params",
		"The number of variable parameter values included in the request did not match the number of variable parameters defined in the template.",
	)

	h, topic := setupMetaAPIErrorTest(t, campID, payload, 400)

	entryID := "entry-param-mismatch"
	ack := h.queueSub.deliver(topic, makePayload(campID, entryID, "5584999990005"))
	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected ack")
	}

	if h.consumeTempl.refunded.Load() != 1 {
		t.Fatalf("expected 1 refund, got %d", h.consumeTempl.refunded.Load())
	}
	if code := h.entryRepo.getErrorCode(entryID); code != 132000 {
		t.Fatalf("expected error code 132000, got %d", code)
	}
}

func TestMetaAPI_AccountRestricted_RefundsAndSetsError(t *testing.T) {

	campID := "camp-meta-368"
	payload := metaAPIError(368,
		"(#368) Temporarily blocked for policies violations",
		"The WhatsApp Business Account associated with the app has been restricted or disabled for violating a platform policy.",
	)

	h, topic := setupMetaAPIErrorTest(t, campID, payload, 403)

	entryID := "entry-restricted"
	ack := h.queueSub.deliver(topic, makePayload(campID, entryID, "5584999990006"))
	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected ack")
	}

	if h.consumeTempl.refunded.Load() != 1 {
		t.Fatalf("expected 1 refund, got %d", h.consumeTempl.refunded.Load())
	}
	if code := h.entryRepo.getErrorCode(entryID); code != 368 {
		t.Fatalf("expected error code 368, got %d", code)
	}
}

func TestMetaAPI_NilResult_RefundsAndSetsErrorFromGoError(t *testing.T) {

	h := newTestHarness()
	campID := "camp-meta-nil"
	topic := makeTopic(campID)

	h.waClient.failSend = true
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = &template.Template{
		ID: "tmpl-1", Name: "test", Status: template.TemplateStatusApproved, Category: "MARKETING",
	}

	h.shared.SetString("campaign:whatsapp:remaining:"+campID, "1", 0)
	h.queueSub.mu.Lock()
	h.queueSub.queueLengths[topic] = 0
	h.queueSub.queueLengths[topic+".delay"] = 0
	h.queueSub.mu.Unlock()

	_ = h.consumer.SubscribeToCampaign(campID)

	entryID := "entry-nil-result"
	ack := h.queueSub.deliver(topic, makePayload(campID, entryID, "5584999990007"))
	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected ack")
	}

	if h.consumeTempl.refunded.Load() != 1 {
		t.Fatalf("expected 1 refund on nil result, got %d", h.consumeTempl.refunded.Load())
	}

	if status := h.entryRepo.getStatus(entryID); status != wce.SendStatusFailed {
		t.Fatalf("expected FAILED, got %s", status)
	}
	if msg := h.entryRepo.getErrorMessage(entryID); msg == "" {
		t.Fatal("expected error message from err.Error() fallback, got empty string")
	}
	if msg := h.entryRepo.getErrorMessage(entryID); msg != "send failed" {
		t.Fatalf("expected 'send failed' from err.Error(), got %q", msg)
	}
}

func TestMetaAPI_UnparseableBody_RefundsAndSetsErrorFromGoError(t *testing.T) {

	campID := "camp-meta-garbage"
	garbagePayload := []byte(`<!DOCTYPE html><html><body>Service Unavailable</body></html>`)

	h, topic := setupMetaAPIErrorTest(t, campID, garbagePayload, 503)

	entryID := "entry-garbage"
	ack := h.queueSub.deliver(topic, makePayload(campID, entryID, "5584999990008"))
	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected ack")
	}

	if h.consumeTempl.refunded.Load() != 1 {
		t.Fatalf("expected 1 refund, got %d", h.consumeTempl.refunded.Load())
	}

	if code := h.entryRepo.getErrorCode(entryID); code != 0 {
		t.Fatalf("expected error code 0 (unparseable), got %d", code)
	}

	if msg := h.entryRepo.getErrorMessage(entryID); msg == "" {
		t.Fatal("expected non-empty error message from err.Error() fallback")
	}
}

func TestMetaAPI_MultipleEntries_EachGetsRefundAndError(t *testing.T) {

	h := newTestHarness()
	campID := "camp-meta-multi-refund"
	topic := makeTopic(campID)

	errorPayload := metaAPIError(131042,
		"(#131042) Business eligibility payment issue",
		"Payment account is not attached to a WhatsApp Business Account",
	)
	h.waClient.failWithPayload = errorPayload
	h.waClient.failWithStatus = 400
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}

	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning,
		WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = &template.Template{
		ID: "tmpl-1", Name: "promo", Status: template.TemplateStatusApproved, Category: "MARKETING",
	}

	h.shared.SetString("campaign:whatsapp:remaining:"+campID, "3", 0)
	h.queueSub.mu.Lock()
	h.queueSub.queueLengths[topic] = 0
	h.queueSub.queueLengths[topic+".delay"] = 0
	h.queueSub.mu.Unlock()
	_ = h.consumer.SubscribeToCampaign(campID)

	for i := 1; i <= 3; i++ {
		entryID := fmt.Sprintf("entry-%d", i)
		phone := fmt.Sprintf("558499900%04d", i)
		ack := h.queueSub.deliver(topic, makePayload(campID, entryID, phone))
		if ack == nil || !ack.acked.Load() {
			t.Fatalf("entry %d: expected ack", i)
		}
	}

	if h.consumeTempl.consumed.Load() != 3 {
		t.Fatalf("expected 3 debits, got %d", h.consumeTempl.consumed.Load())
	}
	if h.consumeTempl.refunded.Load() != 3 {
		t.Fatalf("expected 3 refunds, got %d", h.consumeTempl.refunded.Load())
	}

	for i := 1; i <= 3; i++ {
		entryID := fmt.Sprintf("entry-%d", i)
		if status := h.entryRepo.getStatus(entryID); status != wce.SendStatusFailed {
			t.Fatalf("entry %d: expected FAILED, got %s", i, status)
		}
		if code := h.entryRepo.getErrorCode(entryID); code != 131042 {
			t.Fatalf("entry %d: expected error code 131042, got %d", i, code)
		}
		if msg := h.entryRepo.getErrorMessage(entryID); msg == "" {
			t.Fatalf("entry %d: expected non-empty error message", i)
		}
	}
}

func TestSubscription_GetCostFails_EntryFailedAndAcked(t *testing.T) {

	h := newTestHarness()
	campID := "camp-no-sub-cost"
	topic := setupCampaignWithCounter(h, campID, 1)

	h.consumeTempl.costErr = workspace_plan.ErrSubscriptionNotCurrent

	ack := h.queueSub.deliver(topic, makePayload(campID, "entry-1", "5584999990001"))
	if ack == nil {
		t.Fatal("expected ack to be non-nil")
	}
	if !ack.acked.Load() {
		t.Error("expected message to be acked (permanent failure)")
	}
	if ack.nacked.Load() {
		t.Error("expected NO nack for subscription failure")
	}

	if status := h.entryRepo.getStatus("entry-1"); status != wce.SendStatusFailed {
		t.Fatalf("expected FAILED status, got %s", status)
	}
	if msg := h.entryRepo.getErrorMessage("entry-1"); msg != "no active subscription" {
		t.Fatalf("expected 'no active subscription' error message, got %q", msg)
	}

	delayed := h.queuePub.delayedMessagesFor(topic)
	if len(delayed) != 0 {
		t.Fatalf("expected 0 delayed messages, got %d", len(delayed))
	}

	if h.consumeTempl.consumed.Load() != 0 {
		t.Error("expected no debit when subscription is not current")
	}

	camp, _ := h.campaignRepo.FindByID(campID)
	if camp.Status != wc.CampaignStatusCompleted {
		t.Fatalf("expected COMPLETED status, got %s", camp.Status)
	}
}

func TestSubscription_ExecuteFails_EntryFailedAndAcked(t *testing.T) {

	h := newTestHarness()
	campID := "camp-no-sub-debit"
	topic := setupCampaignWithCounter(h, campID, 1)

	h.consumeTempl.executeErr = workspace_plan.ErrSubscriptionNotCurrent

	ack := h.queueSub.deliver(topic, makePayload(campID, "entry-1", "5584999990001"))
	if ack == nil {
		t.Fatal("expected ack to be non-nil")
	}
	if !ack.acked.Load() {
		t.Error("expected message to be acked (permanent failure)")
	}
	if ack.nacked.Load() {
		t.Error("expected NO nack for subscription failure")
	}

	if status := h.entryRepo.getStatus("entry-1"); status != wce.SendStatusFailed {
		t.Fatalf("expected FAILED status, got %s", status)
	}
	if msg := h.entryRepo.getErrorMessage("entry-1"); msg != "no active subscription" {
		t.Fatalf("expected 'no active subscription' error message, got %q", msg)
	}

	delayed := h.queuePub.delayedMessagesFor(topic)
	if len(delayed) != 0 {
		t.Fatalf("expected 0 delayed messages, got %d", len(delayed))
	}

	camp, _ := h.campaignRepo.FindByID(campID)
	if camp.Status != wc.CampaignStatusCompleted {
		t.Fatalf("expected COMPLETED status, got %s", camp.Status)
	}
}

func TestCounter_SubscriptionExpired_DecrementsCounter(t *testing.T) {

	h := newTestHarness()
	campID := "camp-sub-counter"
	topic := setupCampaignWithCounter(h, campID, 5)

	for i := 1; i <= 2; i++ {
		h.queueSub.deliver(topic, makePayload(campID, fmt.Sprintf("entry-%d", i), fmt.Sprintf("558499900%04d", i)))
	}

	remaining, _ := h.shared.GetString("campaign:whatsapp:remaining:" + campID)
	if remaining != "3" {
		t.Fatalf("expected remaining=3, got %s", remaining)
	}

	h.consumeTempl.costErr = workspace_plan.ErrSubscriptionNotCurrent

	ack := h.queueSub.deliver(topic, makePayload(campID, "entry-3", "5584999000003"))
	if ack == nil || !ack.acked.Load() {
		t.Fatal("expected ack for subscription failure")
	}

	remaining, _ = h.shared.GetString("campaign:whatsapp:remaining:" + campID)
	if remaining != "2" {
		t.Fatalf("expected remaining=2 after subscription failure, got %s", remaining)
	}

	camp, _ := h.campaignRepo.FindByID(campID)
	if camp.Status != wc.CampaignStatusRunning {
		t.Fatalf("expected RUNNING, got %s", camp.Status)
	}
}

func (r *mockBusinessPhoneRepo) UpdateCallsEnabled(string, bool) error { return nil }

func (f *mockWhatsAppClient) SendCallPermissionRequest(context.Context, conversation.SendCallPermissionRequestInput) (*conversation.SendTextMessageOutput, error) {
	return &conversation.SendTextMessageOutput{}, nil
}