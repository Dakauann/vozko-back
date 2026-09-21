package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"sync"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/conversation"
	"vozko/domain/messaging"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	conversation_usecase "vozko/usecases/conversation"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

type fakeCampaignRepo struct {
	mu        sync.Mutex
	campaigns map[string]*uwc.Campaign
	reasons   map[string]string
}

func newFakeCampaignRepo() *fakeCampaignRepo {
	return &fakeCampaignRepo{campaigns: map[string]*uwc.Campaign{}, reasons: map[string]string{}}
}

func (f *fakeCampaignRepo) put(c *uwc.Campaign) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.campaigns[c.ID] = c
}

func (f *fakeCampaignRepo) Create(c *uwc.Campaign) error { f.put(c); return nil }

func (f *fakeCampaignRepo) Update(id string, c *uwc.Campaign) error { f.put(c); return nil }

func (f *fakeCampaignRepo) Delete(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.campaigns, id)
	return nil
}

func (f *fakeCampaignRepo) FindByID(id string) (*uwc.Campaign, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.campaigns[id]
	if !ok {
		return nil, uwc.ErrCampaignNotFound
	}
	clone := *c
	return &clone, nil
}

func (f *fakeCampaignRepo) List(uwc.ListCampaignsInput) (*shared.PaginatedResult[*uwc.Campaign], error) {
	return nil, nil
}

func (f *fakeCampaignRepo) ListByStatus(status campaign.Status) ([]*uwc.Campaign, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*uwc.Campaign
	for _, c := range f.campaigns {
		if c.Status == status {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeCampaignRepo) ListRunningByInstance(instanceID string) ([]*uwc.Campaign, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*uwc.Campaign
	for _, c := range f.campaigns {
		if c.InstanceID == instanceID && c.Status == campaign.StatusRunning {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeCampaignRepo) ListScheduledToStart(time.Time, int) ([]*uwc.Campaign, error) {
	return nil, nil
}

func (f *fakeCampaignRepo) UpdateStatus(id string, status campaign.Status, allowed ...campaign.Status) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.campaigns[id]
	if !ok {
		return false, nil
	}
	if len(allowed) > 0 {
		match := false
		for _, a := range allowed {
			if c.Status == a {
				match = true
			}
		}
		if !match {
			return false, nil
		}
	}
	c.Status = status
	return true, nil
}

func (f *fakeCampaignRepo) UpdateStatusReason(id, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reasons[id] = reason
	return nil
}

func (f *fakeCampaignRepo) reason(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reasons[id]
}

func (f *fakeCampaignRepo) UpdateResetCode(id, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.campaigns[id]; ok {
		c.ResetCode = code
	}
	return nil
}

func (f *fakeCampaignRepo) UpdateClearCode(id, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.campaigns[id]; ok {
		c.ClearCode = code
	}
	return nil
}

type fakeEntryRepo struct {
	mu      sync.Mutex
	entries map[string]*uwc.Entry
	sends   []uwc.RecordSendInput
	checks  int
}

func newFakeEntryRepo() *fakeEntryRepo {
	return &fakeEntryRepo{entries: map[string]*uwc.Entry{}}
}

func (f *fakeEntryRepo) put(e *uwc.Entry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[e.ID] = e
}

func (f *fakeEntryRepo) get(id string) *uwc.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[id]
	if !ok {
		return nil
	}
	clone := *e
	return &clone
}

func (f *fakeEntryRepo) CreateMany(entries []uwc.Entry) ([]uwc.Entry, error) {
	for i := range entries {
		e := entries[i]
		f.put(&e)
	}
	return entries, nil
}

func (f *fakeEntryRepo) FindByID(id string) (*uwc.Entry, error) {
	if e := f.get(id); e != nil {
		return e, nil
	}
	return nil, uwc.ErrEntryNotFound
}

func (f *fakeEntryRepo) FindLatestByConversationID(conversationID string) (*uwc.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best *uwc.Entry
	for _, e := range f.entries {
		if e.ConversationID != conversationID {
			continue
		}
		if best == nil || laterEntry(e, best) {
			best = e
		}
	}
	if best == nil {
		return nil, uwc.ErrEntryNotFound
	}
	clone := *best
	return &clone, nil
}

func laterEntry(a, b *uwc.Entry) bool {
	switch {
	case a.SentAt != nil && b.SentAt == nil:
		return true
	case a.SentAt == nil && b.SentAt != nil:
		return false
	case a.SentAt != nil && b.SentAt != nil && !a.SentAt.Equal(*b.SentAt):
		return a.SentAt.After(*b.SentAt)
	}
	return a.UpdatedAt.After(b.UpdatedAt)
}

func (f *fakeEntryRepo) FindByProviderMessageID(pid string) (*uwc.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.entries {
		if e.ProviderMessageID == pid {
			clone := *e
			return &clone, nil
		}
	}
	return nil, uwc.ErrEntryNotFound
}

func (f *fakeEntryRepo) FindByCampaignAndLead(string, string) (*uwc.Entry, error) {
	return nil, uwc.ErrEntryNotFound
}
func (f *fakeEntryRepo) Delete(string) error             { return nil }
func (f *fakeEntryRepo) DeleteByCampaignID(string) error { return nil }

func (f *fakeEntryRepo) List(uwc.ListEntriesInput) (*shared.PaginatedResult[*uwc.EntryWithLead], error) {
	return nil, nil
}

func (f *fakeEntryRepo) ListByStatus(campaignID string, status campaign.SendStatus, limit int) ([]uwc.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []uwc.Entry
	for _, e := range f.entries {
		if e.CampaignID == campaignID && e.Status == status {
			out = append(out, *e)
		}
	}
	return out, nil
}

func (f *fakeEntryRepo) ListRecentlyUpdated(string, int) ([]uwc.Entry, error) { return nil, nil }

func (f *fakeEntryRepo) CountByStatus(campaignID string) (*campaign.Counts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	counts := &campaign.Counts{}
	for _, e := range f.entries {
		if e.CampaignID != campaignID {
			continue
		}
		counts.Total++
		if e.Status == campaign.SendStatusPending {
			counts.Pending++
		}
	}
	return counts, nil
}

func (f *fakeEntryRepo) CountByStatusForCampaigns([]string) (map[string]*campaign.Counts, error) {
	return map[string]*campaign.Counts{}, nil
}

func (f *fakeEntryRepo) UpdateStatus(id string, status campaign.SendStatus, pid string, code int, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.entries[id]; ok {
		e.Status = status
		e.ErrorCode = code
		e.ErrorMessage = msg
		if pid != "" {
			e.ProviderMessageID = pid
		}
	}
	return nil
}

func (f *fakeEntryRepo) UpdateStatusByProviderMessageID(pid string, status campaign.SendStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.entries {
		if e.ProviderMessageID == pid {
			e.Status = status
		}
	}
	return nil
}

func (f *fakeEntryRepo) RecordCheck(id, jid string, at time.Time, onWhatsApp bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	if e, ok := f.entries[id]; ok {
		e.JID = jid
		e.CheckedAt = &at
		if !onWhatsApp {
			e.Status = campaign.SendStatusSkippedNotOnWhatsApp
		}
	}
	return nil
}

func (f *fakeEntryRepo) RecordSend(id string, in uwc.RecordSendInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, in)
	if e, ok := f.entries[id]; ok {
		e.Status = campaign.SendStatusSent
		e.ProviderMessageID = in.ProviderMessageID
		e.ConversationID = in.ConversationID
		e.VariantIndex = in.VariantIndex
	}
	return nil
}

func (f *fakeEntryRepo) ResetAllStatuses(string) (int64, error) { return 0, nil }
func (f *fakeEntryRepo) UpdateEntryDetails(entryID string, in uwc.UpdateEntryDetails) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[entryID]
	if !ok {
		return uwc.ErrEntryNotFound
	}
	for id, other := range f.entries {
		if id != entryID && other.CampaignID == e.CampaignID && other.LeadID == in.LeadID {
			return uwc.ErrEntryDuplicate
		}
	}
	e.LeadID, e.Number, e.Name = in.LeadID, in.Number, in.Name
	e.Variables, e.Metadata = in.Variables, in.Metadata
	return nil
}

func (f *fakeEntryRepo) UpsertEntries(campaignID string, entries []uwc.Entry) error {
	for i := range entries {
		e := entries[i]
		e.CampaignID = campaignID
		f.put(&e)
	}
	return nil
}
func (f *fakeEntryRepo) ConversationIDsForCampaign(string) ([]string, error) { return nil, nil }

type fakeGateway struct {
	instance    *uw.Instance
	checkResult []uw.NumberCheck
	checkErr    error
	checkCalls  int
	limits      *uw.Restriction
	limitsErr   error
	cached      []uw.Restriction
	resolveErr  error
}

func (f *fakeGateway) Instance(context.Context, string) (*uw.Instance, error) {
	if f.instance == nil {
		return nil, uw.ErrInstanceNotFound
	}
	return f.instance, nil
}

func (f *fakeGateway) Ref(context.Context, *uw.Instance) (uw.InstanceRef, error) {
	return uw.InstanceRef{}, nil
}

func (f *fakeGateway) CheckNumbers(_ context.Context, _ uw.InstanceRef, numbers []string) ([]uw.NumberCheck, error) {
	f.checkCalls++
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	if f.checkResult != nil {
		return f.checkResult, nil
	}
	out := make([]uw.NumberCheck, 0, len(numbers))
	for _, n := range numbers {
		out = append(out, uw.NumberCheck{Query: n, JID: n + "@s.whatsapp.net", IsOnWhatsApp: true})
	}
	return out, nil
}

func (f *fakeGateway) MessagingLimits(context.Context, uw.InstanceRef) (*uw.Restriction, error) {
	return f.limits, f.limitsErr
}

func (f *fakeGateway) CacheRestriction(_ context.Context, _ string, r uw.Restriction) error {
	f.cached = append(f.cached, r)
	return nil
}

func (f *fakeGateway) Resolve(_ context.Context, instance *uw.Instance, in uwuc.ResolveInput) (*uwuc.Resolved, error) {
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	return &uwuc.Resolved{
		Conversation: &uw.Conversation{ID: "conv-" + in.PhoneNumber, InstanceID: instance.ID},
		Contact:      &uw.Contact{ID: "contact-" + in.PhoneNumber, PhoneNumber: in.PhoneNumber},
	}, nil
}

type fakeSender struct {
	mu   sync.Mutex
	sent []conversation_usecase.SendCampaignMessageInput
	err  error
}

func (f *fakeSender) SendCampaignMessage(in conversation_usecase.SendCampaignMessageInput) (*conversation.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.sent = append(f.sent, in)
	pid := "prov-" + in.EntryID
	return &conversation.Message{ID: "msg-" + in.EntryID, ExternalMessageID: &pid}, nil
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeSender) last() conversation_usecase.SendCampaignMessageInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sent[len(f.sent)-1]
}

type fakeAssigner struct{ calls []string }

func (f *fakeAssigner) EnsureAssignment(entryID, entryType, accountID string) string {
	f.calls = append(f.calls, entryID)
	return "user-1"
}

type fakeSpam struct {
	skip     map[string]bool
	recorded []string
}

func (f *fakeSpam) ShouldSkip(_ context.Context, _, leadID, _ string) bool { return f.skip[leadID] }
func (f *fakeSpam) Record(leadID, senderID, campaignID string) error {
	f.recorded = append(f.recorded, leadID)
	return nil
}
func (f *fakeSpam) SkipMany(_ context.Context, _ string, leadIDs []string, _ string) map[string]bool {
	out := map[string]bool{}
	for _, id := range leadIDs {
		if f.skip[id] {
			out[id] = true
		}
	}
	return out
}

type fakeMetrics struct{ recorded []RecordSendMetric }

func (f *fakeMetrics) RecordCampaignSend(in RecordSendMetric) { f.recorded = append(f.recorded, in) }

type fakeWorkflows struct{ fired []CampaignSentTrigger }

func (f *fakeWorkflows) CampaignSent(in CampaignSentTrigger) { f.fired = append(f.fired, in) }

type fakePauser struct {
	calls   int
	reasons []string
}

func (f *fakePauser) Execute(_ context.Context, instanceID, reason string) (int, error) {
	f.calls++
	f.reasons = append(f.reasons, reason)
	return 1, nil
}

type noopQueueSub struct{}

func (noopQueueSub) Subscribe(string, func([]byte, messaging.MessageAck)) error { return nil }
func (noopQueueSub) DeleteQueue(string) error                                   { return nil }
func (noopQueueSub) ValidateConnection() error                                  { return nil }
func (noopQueueSub) GetQueueLength(string) (int, error)                         { return 0, nil }

type noopQueuePub struct{}

func (noopQueuePub) Publish(string, []byte) error                         { return nil }
func (noopQueuePub) PublishWithDelay(string, []byte, time.Duration) error { return nil }
func (noopQueuePub) ValidateConnection() error                            { return nil }

var errBoom = errors.New("boom")
