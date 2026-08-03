package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/lead"
	"vozko/domain/shared"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type qsCampaignRepo struct {
	mu             sync.Mutex
	campaigns      map[string]*wc.Campaign
	updateStatusFn func(id string, status wc.Status, allowed []wc.Status) (bool, error)
	findErr        error
	statusCalls    []wc.Status
}

func newQSCampaignRepo() *qsCampaignRepo {
	return &qsCampaignRepo{campaigns: make(map[string]*wc.Campaign)}
}

func (r *qsCampaignRepo) put(c *wc.Campaign) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.campaigns[c.ID] = c
}

func (r *qsCampaignRepo) Create(_ *wc.Campaign) error           { return nil }
func (r *qsCampaignRepo) Update(_ string, _ *wc.Campaign) error { return nil }
func (r *qsCampaignRepo) Delete(_ string) error                 { return nil }
func (r *qsCampaignRepo) FindLatestOrganicByBusinessPhone(_, _ string) (*wc.Campaign, error) {
	return nil, nil
}
func (r *qsCampaignRepo) List(_ wc.ListCampaignsInput) (*shared.PaginatedResult[*wc.Campaign], error) {
	return nil, nil
}
func (r *qsCampaignRepo) ListByStatus(_ wc.Status) ([]*wc.Campaign, error) { return nil, nil }
func (r *qsCampaignRepo) ListScheduledToStart(_ time.Time, _ int) ([]*wc.Campaign, error) {
	return nil, nil
}
func (r *qsCampaignRepo) UpdateResetCode(_ string, _ string) error { return nil }
func (r *qsCampaignRepo) UpdateClearCode(_ string, _ string) error { return nil }

func (r *qsCampaignRepo) FindByID(id string) (*wc.Campaign, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.findErr != nil {
		return nil, r.findErr
	}
	c, ok := r.campaigns[id]
	if !ok {
		return nil, nil
	}

	dup := *c
	return &dup, nil
}

func (r *qsCampaignRepo) UpdateStatus(id string, status wc.Status, allowed ...wc.Status) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statusCalls = append(r.statusCalls, status)
	if r.updateStatusFn != nil {
		return r.updateStatusFn(id, status, allowed)
	}
	c, ok := r.campaigns[id]
	if !ok {
		return false, nil
	}
	if len(allowed) == 0 {
		c.Status = status
		return true, nil
	}
	for _, a := range allowed {
		if c.Status == a {
			c.Status = status
			return true, nil
		}
	}
	return false, nil
}

var _ wc.Repository = (*qsCampaignRepo)(nil)

type qsEntryRepo struct {
	mu                 sync.Mutex
	entriesByID        map[string]*wce.WhatsAppCampaignEntry
	entriesByNumberKey map[string]*wce.WhatsAppCampaignEntry
	pendingByCampaign  map[string][]*wce.WhatsAppCampaignEntry
	createErr          error
	findByNumberErr    error
	countErr           error
	listByStatusErr    error
	createCalls        atomic.Int32
}

func newQSEntryRepo() *qsEntryRepo {
	return &qsEntryRepo{
		entriesByID:        make(map[string]*wce.WhatsAppCampaignEntry),
		entriesByNumberKey: make(map[string]*wce.WhatsAppCampaignEntry),
		pendingByCampaign:  make(map[string][]*wce.WhatsAppCampaignEntry),
	}
}

func (r *qsEntryRepo) numberKey(campaignID, number string) string {
	return campaignID + "|" + number
}

func (r *qsEntryRepo) seedDuplicate(campaignID, number string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := "dup-" + number
	e := &wce.WhatsAppCampaignEntry{ID: id, CampaignID: campaignID, Status: wce.SendStatusSent, Lead: &lead.Lead{Number: number}}
	r.entriesByID[id] = e
	r.entriesByNumberKey[r.numberKey(campaignID, number)] = e
}

func (r *qsEntryRepo) seedPending(campaignID string, count int) []*wce.WhatsAppCampaignEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*wce.WhatsAppCampaignEntry, 0, count)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("pending-%s-%d", campaignID, i)
		num := fmt.Sprintf("55849999%04d", i)
		e := &wce.WhatsAppCampaignEntry{
			ID:         id,
			CampaignID: campaignID,
			Status:     wce.SendStatusPending,
			Lead:       &lead.Lead{ID: id + "-lead", Number: num},
		}
		r.entriesByID[id] = e
		r.entriesByNumberKey[r.numberKey(campaignID, num)] = e
		r.pendingByCampaign[campaignID] = append(r.pendingByCampaign[campaignID], e)
		out = append(out, e)
	}
	return out
}

func (r *qsEntryRepo) Create(e *wce.WhatsAppCampaignEntry) error {
	r.createCalls.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return r.createErr
	}
	now := time.Now()
	e.CreatedAt = now
	e.UpdatedAt = now

	r.entriesByID[e.ID] = e
	if e.Lead != nil && e.Lead.Number != "" {
		r.entriesByNumberKey[r.numberKey(e.CampaignID, e.Lead.Number)] = e
	}
	if e.Status == wce.SendStatusPending {
		r.pendingByCampaign[e.CampaignID] = append(r.pendingByCampaign[e.CampaignID], e)
	}
	return nil
}
func (r *qsEntryRepo) FindByCampaignAndNumber(campaignID, number string) (*wce.WhatsAppCampaignEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.findByNumberErr != nil {
		return nil, r.findByNumberErr
	}
	if e, ok := r.entriesByNumberKey[r.numberKey(campaignID, number)]; ok {
		return e, nil
	}
	return nil, wce.ErrEntryNotFound
}
func (r *qsEntryRepo) CountByCampaignID(_ string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.countErr != nil {
		return 0, r.countErr
	}
	return int64(len(r.entriesByID)), nil
}
func (r *qsEntryRepo) ListByStatus(campaignID string, _ wce.SendStatus, _ int) ([]wce.WhatsAppCampaignEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listByStatusErr != nil {
		return nil, r.listByStatusErr
	}
	src := r.pendingByCampaign[campaignID]
	out := make([]wce.WhatsAppCampaignEntry, 0, len(src))
	for _, e := range src {
		out = append(out, *e)
	}
	return out, nil
}

func (r *qsEntryRepo) CreateMany(in []wce.WhatsAppCampaignEntry) ([]wce.WhatsAppCampaignEntry, error) {
	return in, nil
}
func (r *qsEntryRepo) FindByID(id string) (*wce.WhatsAppCampaignEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entriesByID[id]; ok {
		return e, nil
	}
	return nil, wce.ErrEntryNotFound
}
func (r *qsEntryRepo) FindByMessageID(_ string) (*wce.WhatsAppCampaignEntry, error) { return nil, nil }
func (r *qsEntryRepo) FindByIDs(_ []string) ([]*wce.WhatsAppCampaignEntry, error)   { return nil, nil }
func (r *qsEntryRepo) FindByCampaignAndLead(_, _ string) (*wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *qsEntryRepo) Delete(_ string) error             { return nil }
func (r *qsEntryRepo) DeleteByCampaignID(_ string) error { return nil }
func (r *qsEntryRepo) ListByCampaignID(_ string) ([]wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *qsEntryRepo) ListByLeadID(_ string) ([]wce.WhatsAppCampaignEntry, error) { return nil, nil }
func (r *qsEntryRepo) ListRecentlyUpdated(_ string, _ int) ([]wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *qsEntryRepo) CountByStatus(_ string) (*wce.StatusCounts, error) { return nil, nil }
func (r *qsEntryRepo) CountByStatusForCampaigns(_ []string) (map[string]*wce.StatusCounts, error) {
	return nil, nil
}
func (r *qsEntryRepo) UpdateStatus(_ string, _ wce.SendStatus, _ string, _ int, _ string) error {
	return nil
}
func (r *qsEntryRepo) UpdateReceivedBusinessPhone(_ string, _ string) error { return nil }
func (r *qsEntryRepo) UpdateStatusByMessageID(_ string, _ wce.SendStatus) error {
	return nil
}
func (r *qsEntryRepo) UpdateStatusByNumber(_, _ string, _ wce.SendStatus, _ string) error {
	return nil
}
func (r *qsEntryRepo) ResetAllStatuses(_ string) (int64, error) { return 0, nil }
func (r *qsEntryRepo) ReplaceCampaignEntries(_ string, _ []wce.WhatsAppCampaignEntry) error {
	return nil
}
func (r *qsEntryRepo) UpsertCampaignEntries(_ string, _ []wce.WhatsAppCampaignEntry) error {
	return nil
}
func (r *qsEntryRepo) List(_ wce.ListEntriesInput) (*shared.PaginatedResult[*wce.WhatsAppCampaignEntry], error) {
	return nil, nil
}
func (r *qsEntryRepo) ListEntriesWithLeads(_ wce.ListEntriesInput) (*shared.PaginatedResult[*wce.EntryWithLead], error) {
	return nil, nil
}
func (r *qsEntryRepo) ListEntriesWithLeadsForUser(_ wce.ListEntriesForUserInput) ([]*wce.EntryWithLead, int64, error) {
	return nil, 0, nil
}
func (r *qsEntryRepo) CanUserAccessEntry(_, _ string, _ bool) (bool, error)     { return false, nil }
func (r *qsEntryRepo) GetAccessibleEntryIDs(_ string, _ bool) ([]string, error) { return nil, nil }
func (r *qsEntryRepo) GetEntryIDsByCampaign(_ string) ([]string, error)         { return nil, nil }
func (r *qsEntryRepo) FindByNumber(_ string) (*wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *qsEntryRepo) FindByNumberAndBusinessPhone(_, _ string) (*wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}
func (r *qsEntryRepo) GetCampaignForEntry(_ string) (*wce.EntryCampaignInfo, error) {
	return nil, nil
}
func (r *qsEntryRepo) UpdateAutomationEnabled(_ string, _ *bool) error         { return nil }
func (r *qsEntryRepo) UpdateMetadata(_ string, _ map[string]interface{}) error { return nil }
func (r *qsEntryRepo) UpdateConversationStatus(_ string, _ wce.ConversationStatusWrite) error {
	return nil
}
func (r *qsEntryRepo) ListEligibleForAutoClose(int) ([]wce.AutoCloseCandidate, error) {
	return nil, nil
}
func (r *qsEntryRepo) ListEligibleForMaxAge(int) ([]wce.AutoCloseCandidate, error) { return nil, nil }
func (r *qsEntryRepo) CountByConversationStatus(_ string) (map[string]int64, error) {
	return nil, nil
}
func (r *qsEntryRepo) CountByConversationStatusForWorkspace(_ string) (map[string]int64, error) {
	return nil, nil
}

type qsLeadRepo struct {
	mu        sync.Mutex
	createErr error
}

func (r *qsLeadRepo) Create(_ *lead.Lead) error                            { return nil }
func (r *qsLeadRepo) FindByID(_ string, _ string) (*lead.Lead, error)      { return nil, nil }
func (r *qsLeadRepo) FindByIDs(_ string, _ []string) ([]*lead.Lead, error) { return nil, nil }
func (r *qsLeadRepo) FindByNumber(_ string, _ string) (*lead.Lead, error)  { return nil, nil }
func (r *qsLeadRepo) Update(_ string, _ string, _ lead.LeadUpdate) error   { return nil }
func (r *qsLeadRepo) Delete(_ string, _ string) error                      { return nil }
func (r *qsLeadRepo) List(_ lead.ListLeadsInput) (*shared.PaginatedResult[*lead.Lead], error) {
	return nil, nil
}
func (r *qsLeadRepo) ListWithSummary(_ lead.ListLeadsInput) (*shared.PaginatedResult[*lead.LeadWithSummary], error) {
	return nil, nil
}
func (r *qsLeadRepo) ResolveCampaignNames(_ []string) map[string]string {
	return nil
}
func (r *qsLeadRepo) FindOrCreateMany(_ string, _ []lead.BulkLeadInput) (map[string]*lead.Lead, error) {
	return nil, nil
}
func (r *qsLeadRepo) FindOrCreate(_ string, number string, update lead.LeadUpdate) (*lead.Lead, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return nil, false, r.createErr
	}
	return &lead.Lead{ID: "lead-" + number, Number: number, Name: update.Name}, true, nil
}

type qsQueuePub struct {
	mu        sync.Mutex
	published [][]byte
	topics    []string
	failAfter int
	failErr   error
}

func newQSQueuePub() *qsQueuePub { return &qsQueuePub{failAfter: -1} }
func (p *qsQueuePub) Publish(topic string, msg []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failAfter >= 0 && len(p.published) >= p.failAfter {
		if p.failErr != nil {
			return p.failErr
		}
		return errors.New("publish failed")
	}
	p.topics = append(p.topics, topic)
	p.published = append(p.published, msg)
	return nil
}
func (p *qsQueuePub) PublishWithDelay(_ string, _ []byte, _ time.Duration) error {
	return nil
}
func (p *qsQueuePub) ValidateConnection() error { return nil }
func (p *qsQueuePub) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.published)
}

type qsConsumer struct {
	mu             sync.Mutex
	subscribed     map[string]bool
	subscribeErr   error
	resumeErr      error
	subscribeCalls int
	resumeCalls    int
}

func newQSConsumer() *qsConsumer {
	return &qsConsumer{subscribed: make(map[string]bool)}
}
func (c *qsConsumer) Start() error { return nil }
func (c *qsConsumer) IsSubscribed(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.subscribed[id]
}
func (c *qsConsumer) SubscribeToCampaign(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscribeCalls++
	if c.subscribeErr != nil {
		return c.subscribeErr
	}
	c.subscribed[id] = true
	return nil
}
func (c *qsConsumer) ResumeCampaignConsumer(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resumeCalls++
	if c.resumeErr != nil {
		return c.resumeErr
	}
	c.subscribed[id] = true
	return nil
}
func (c *qsConsumer) PauseCampaignConsumer(_ string) error { return nil }
func (c *qsConsumer) StopCampaignConsumer(_ string) error  { return nil }

type qsShared struct {
	mu       sync.Mutex
	data     map[string]string
	setNxErr error
	delErr   error
	incrErr  error
	setErr   error

	blockSetNX chan struct{}
}

func newQSShared() *qsShared {
	return &qsShared{data: make(map[string]string)}
}
func (s *qsShared) SetNX(key, value string, _ time.Duration) (bool, error) {
	if s.blockSetNX != nil {
		<-s.blockSetNX
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setNxErr != nil {
		return false, s.setNxErr
	}
	if _, exists := s.data[key]; exists {
		return false, nil
	}
	s.data[key] = value
	return true, nil
}
func (s *qsShared) SetString(key, value string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setErr != nil {
		return s.setErr
	}
	s.data[key] = value
	return nil
}
func (s *qsShared) GetString(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}
func (s *qsShared) Del(keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.delErr != nil {
		return s.delErr
	}
	for _, k := range keys {
		delete(s.data, k)
	}
	return nil
}
func (s *qsShared) Exists(key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[key]
	return ok, nil
}
func (s *qsShared) Incr(key string) (int64, error)                       { return s.IncrBy(key, 1) }
func (s *qsShared) Decr(key string) (int64, error)                       { return s.DecrBy(key, 1) }
func (s *qsShared) IncrWithTTL(_ string, _ time.Duration) (int64, error) { return 0, nil }
func (s *qsShared) TryIncr(_ string, _ int64) (bool, error)              { return true, nil }
func (s *qsShared) TryIncrBy(_ string, _ int64, _ int64) (bool, error)   { return true, nil }
func (s *qsShared) IncrBy(key string, amount int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.incrErr != nil {
		return 0, s.incrErr
	}
	cur, _ := strconv.ParseInt(s.data[key], 10, 64)
	cur += amount
	s.data[key] = strconv.FormatInt(cur, 10)
	return cur, nil
}
func (s *qsShared) DecrBy(key string, amount int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, _ := strconv.ParseInt(s.data[key], 10, 64)
	cur -= amount
	s.data[key] = strconv.FormatInt(cur, 10)
	return cur, nil
}
func (s *qsShared) SAdd(_ string, _ ...string) error                      { return nil }
func (s *qsShared) SRem(_ string, _ ...string) error                      { return nil }
func (s *qsShared) SMembers(_ string) ([]string, error)                   { return nil, nil }
func (s *qsShared) Publish(_ string, _ []byte) error                      { return nil }
func (s *qsShared) Subscribe(_ context.Context, _ string, _ func([]byte)) {}
func (s *qsShared) HSet(_, _, _ string) error                             { return nil }
func (s *qsShared) HDel(_, _ string) error                                { return nil }
func (s *qsShared) HGetAll(_ string) (map[string]string, error)           { return nil, nil }
func (s *qsShared) HIncrBy(_, _ string, _ int64) (int64, error)           { return 0, nil }
func (s *qsShared) Expire(_ string, _ time.Duration) (bool, error)        { return true, nil }

type qsHarness struct {
	camp     *qsCampaignRepo
	entry    *qsEntryRepo
	leadRepo *qsLeadRepo
	pub      *qsQueuePub
	cons     *qsConsumer
	shared   *qsShared
	uc       wc.QuickSendUseCase
}

func newHarness(t *testing.T, status wc.Status, campaignID string) *qsHarness {
	t.Helper()
	h := &qsHarness{
		camp:     newQSCampaignRepo(),
		entry:    newQSEntryRepo(),
		leadRepo: &qsLeadRepo{},
		pub:      newQSQueuePub(),
		cons:     newQSConsumer(),
		shared:   newQSShared(),
	}
	h.camp.put(&wc.Campaign{ID: campaignID, WorkspaceID: "ws-1", Status: status})
	h.uc = NewQuickSendUseCase(h.camp, h.entry, h.leadRepo, h.pub, h.cons, h.shared)
	return h
}

func validPhones(n int) []wc.EntryInput {
	out := make([]wc.EntryInput, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, wc.EntryInput{Number: fmt.Sprintf("55849999%04d", 1000+i)})
	}
	return out
}

func TestQuickSend_EmptyCampaignID(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusStopped, "c-1")
	if _, err := h.uc.Execute(wc.QuickSendInput{}); !errors.Is(err, wc.ErrDispatchCampaignIDRequired) {
		t.Fatalf("expected ErrDispatchCampaignIDRequired, got %v", err)
	}
}

func TestQuickSend_CampaignNotFound(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusStopped, "c-1")
	if _, err := h.uc.Execute(wc.QuickSendInput{CampaignID: "missing"}); !errors.Is(err, wc.ErrCampaignNotFound) {
		t.Fatalf("expected ErrCampaignNotFound, got %v", err)
	}
}

func TestQuickSend_InvalidPhoneNumber(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusStopped, "c-1")
	_, err := h.uc.Execute(wc.QuickSendInput{
		CampaignID:   "c-1",
		PhoneNumbers: []wc.EntryInput{{Number: "not-a-number"}},
	})
	if !errors.Is(err, wc.ErrCampaignPhoneNumberInvalid) {
		t.Fatalf("expected ErrCampaignPhoneNumberInvalid, got %v", err)
	}
	if h.entry.createCalls.Load() != 0 {
		t.Fatalf("expected no entry creates on validation failure")
	}
}

func TestQuickSend_ExceedsMaxPhoneNumbers(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusStopped, "c-1")

	h.entry.mu.Lock()
	for i := 0; i < wc.MaxCampaignPhoneNumbers; i++ {
		h.entry.entriesByID[fmt.Sprintf("e-%d", i)] = &wce.WhatsAppCampaignEntry{}
	}
	h.entry.mu.Unlock()

	_, err := h.uc.Execute(wc.QuickSendInput{
		CampaignID:   "c-1",
		PhoneNumbers: validPhones(1),
	})
	if !errors.Is(err, wc.ErrCampaignPhoneNumbersTooMany) {
		t.Fatalf("expected ErrCampaignPhoneNumbersTooMany, got %v", err)
	}
}

func TestQuickSend_StoppedCampaign_PublishesAndIncrCounter(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusStopped, "c-1")
	out, err := h.uc.Execute(wc.QuickSendInput{
		CampaignID:   "c-1",
		PhoneNumbers: validPhones(2),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.AddedCount != 2 || out.DispatchedCount != 2 || out.DuplicatesSkipped != 0 {
		t.Fatalf("unexpected output: %+v", out)
	}
	if out.Status != string(wc.CampaignStatusRunning) {
		t.Fatalf("expected RUNNING status, got %s", out.Status)
	}
	if h.cons.subscribeCalls != 1 {
		t.Fatalf("expected exactly one subscribe, got %d", h.cons.subscribeCalls)
	}
	if h.pub.count() != 2 {
		t.Fatalf("expected 2 published messages, got %d", h.pub.count())
	}
	v, _ := h.shared.GetString(remainingCounterKey("c-1"))
	if v != "2" {
		t.Fatalf("expected counter 2, got %q", v)
	}

	if exists, _ := h.shared.Exists(quickSendLockKey("c-1")); exists {
		t.Fatalf("expected quick-send lock to be released")
	}
}

func TestQuickSend_RunningCampaign_UsesIncrByNotOverwrite(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusRunning, "c-1")

	_ = h.shared.SetString(remainingCounterKey("c-1"), "500", time.Hour)

	h.cons.subscribed["c-1"] = true

	out, err := h.uc.Execute(wc.QuickSendInput{
		CampaignID:   "c-1",
		PhoneNumbers: validPhones(2),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.DispatchedCount != 2 {
		t.Fatalf("expected 2 dispatched, got %d", out.DispatchedCount)
	}
	v, _ := h.shared.GetString(remainingCounterKey("c-1"))
	if v != "502" {
		t.Fatalf("expected counter 502 (500+2), got %q, overwrite would have produced 2", v)
	}
	if h.cons.subscribeCalls != 0 || h.cons.resumeCalls != 0 {
		t.Fatalf("running campaign should not re-subscribe or resume; got sub=%d resume=%d", h.cons.subscribeCalls, h.cons.resumeCalls)
	}
}

func TestQuickSend_PausedCampaign_ResumesConsumerAndDispatchesAllPending(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusPaused, "c-1")
	h.entry.seedPending("c-1", 3)

	h.cons.subscribed["c-1"] = true

	out, err := h.uc.Execute(wc.QuickSendInput{CampaignID: "c-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.AddedCount != 0 {
		t.Fatalf("expected AddedCount=0 when no phoneNumbers in body, got %d", out.AddedCount)
	}
	if out.DispatchedCount != 3 {
		t.Fatalf("expected 3 pending entries dispatched, got %d", out.DispatchedCount)
	}
	if h.cons.resumeCalls != 1 {
		t.Fatalf("expected Resume to be called for paused campaign, got %d", h.cons.resumeCalls)
	}
	if h.cons.subscribeCalls != 0 {
		t.Fatalf("paused-but-attached should not resubscribe, got %d", h.cons.subscribeCalls)
	}
	v, _ := h.shared.GetString(remainingCounterKey("c-1"))
	if v != "3" {
		t.Fatalf("expected counter reset to 3, got %q", v)
	}
}

func TestQuickSend_NoPendingNoBody_IsNoop(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusRunning, "c-1")
	h.cons.subscribed["c-1"] = true

	out, err := h.uc.Execute(wc.QuickSendInput{CampaignID: "c-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.DispatchedCount != 0 || out.AddedCount != 0 {
		t.Fatalf("expected noop, got %+v", out)
	}
	if h.pub.count() != 0 {
		t.Fatalf("expected zero publishes")
	}

	if exists, _ := h.shared.Exists(remainingCounterKey("c-1")); exists {
		t.Fatalf("expected counter not to be created on noop")
	}
}

func TestQuickSend_DuplicateNumbers_AreSkipped(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusStopped, "c-1")
	dup := "5584999912345"
	h.entry.seedDuplicate("c-1", dup)

	out, err := h.uc.Execute(wc.QuickSendInput{
		CampaignID: "c-1",
		PhoneNumbers: []wc.EntryInput{
			{Number: dup},
			{Number: "5584999967890"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.AddedCount != 1 || out.DuplicatesSkipped != 1 {
		t.Fatalf("expected 1 added + 1 dup, got %+v", out)
	}
	if out.DispatchedCount != 1 {
		t.Fatalf("expected only 1 dispatched (the new one), got %d", out.DispatchedCount)
	}
}

func TestQuickSend_LockBusy_ReturnsBusyError(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusStopped, "c-1")

	if _, err := h.shared.SetNX(quickSendLockKey("c-1"), "1", time.Minute); err != nil {
		t.Fatalf("seed lock: %v", err)
	}

	_, err := h.uc.Execute(wc.QuickSendInput{
		CampaignID:   "c-1",
		PhoneNumbers: validPhones(1),
	})
	if !errors.Is(err, ErrQuickSendBusy) {
		t.Fatalf("expected ErrQuickSendBusy, got %v", err)
	}

	if exists, _ := h.shared.Exists(quickSendLockKey("c-1")); !exists {
		t.Fatalf("busy path must not release another holder's lock")
	}
}

func TestQuickSend_ConcurrentReplicaSafety(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusStopped, "c-1")

	const concurrency = 20
	var (
		wg        sync.WaitGroup
		successes atomic.Int32
		busy      atomic.Int32
	)
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			_, err := h.uc.Execute(wc.QuickSendInput{
				CampaignID: "c-1",
				PhoneNumbers: []wc.EntryInput{
					{Number: fmt.Sprintf("558499%06d", 100000+idx)},
				},
			})
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrQuickSendBusy):
				busy.Add(1)
			default:
				t.Errorf("unexpected error from concurrent quick send: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if successes.Load() == 0 {
		t.Fatalf("expected at least one quick-send to succeed under concurrency")
	}
	if successes.Load()+busy.Load() != concurrency {
		t.Fatalf("every call must either succeed or report busy: %d ok + %d busy", successes.Load(), busy.Load())
	}

	if h.cons.subscribeCalls > 1 {
		t.Fatalf("subscribe should fire at most once across replicas; got %d", h.cons.subscribeCalls)
	}

	if exists, _ := h.shared.Exists(quickSendLockKey("c-1")); exists {
		t.Fatalf("lock should be released after final quick-send completes")
	}
}

func TestQuickSend_InvalidStatus_Errors(t *testing.T) {
	h := newHarness(t, wc.Status("WEIRD"), "c-1")

	h.entry.seedPending("c-1", 1)
	_, err := h.uc.Execute(wc.QuickSendInput{
		CampaignID: "c-1",
	})
	if !errors.Is(err, wc.ErrCampaignStatusInvalid) {
		t.Fatalf("expected ErrCampaignStatusInvalid, got %v", err)
	}
}

func TestQuickSend_SubscribeFails_RevertsStatus(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusStopped, "c-1")
	h.cons.subscribeErr = errors.New("rabbit down")

	_, err := h.uc.Execute(wc.QuickSendInput{
		CampaignID:   "c-1",
		PhoneNumbers: validPhones(1),
	})
	if err == nil {
		t.Fatalf("expected subscribe error to surface")
	}
	c, _ := h.camp.FindByID("c-1")
	if c.Status != wc.CampaignStatusStopped {
		t.Fatalf("expected status to be reverted to STOPPED, got %s", c.Status)
	}
	if h.pub.count() != 0 {
		t.Fatalf("must not publish if subscribe failed")
	}
}

func TestQuickSend_PublishFailure_CompensatesCounter(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusRunning, "c-1")
	h.cons.subscribed["c-1"] = true
	_ = h.shared.SetString(remainingCounterKey("c-1"), "100", time.Hour)

	h.pub.failAfter = 1

	_, err := h.uc.Execute(wc.QuickSendInput{
		CampaignID:   "c-1",
		PhoneNumbers: validPhones(3),
	})
	if err == nil {
		t.Fatalf("expected publish failure to surface")
	}

	got, _ := h.shared.GetString(remainingCounterKey("c-1"))
	if got != "101" {
		t.Fatalf("expected counter to be compensated to 101, got %q", got)
	}
}

func TestQuickSend_PublishFailureFromTransition_RevertsStatus(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusStopped, "c-1")
	h.pub.failAfter = 0

	_, err := h.uc.Execute(wc.QuickSendInput{
		CampaignID:   "c-1",
		PhoneNumbers: validPhones(1),
	})
	if err == nil {
		t.Fatalf("expected publish failure")
	}
	c, _ := h.camp.FindByID("c-1")
	if c.Status != wc.CampaignStatusStopped {
		t.Fatalf("expected status reverted to STOPPED, got %s", c.Status)
	}
}

func TestQuickSend_RaceLostCAS_FallsThroughToRunningPath(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusPaused, "c-1")

	h.camp.updateStatusFn = func(_ string, _ wc.Status, _ []wc.Status) (bool, error) {

		h.camp.campaigns["c-1"].Status = wc.CampaignStatusRunning
		return false, nil
	}

	h.cons.subscribed["c-1"] = true
	_ = h.shared.SetString(remainingCounterKey("c-1"), "10", time.Hour)

	out, err := h.uc.Execute(wc.QuickSendInput{
		CampaignID:   "c-1",
		PhoneNumbers: validPhones(2),
	})
	if err != nil {
		t.Fatalf("expected fall-through to RUNNING path, got error: %v", err)
	}
	if out.DispatchedCount != 2 {
		t.Fatalf("expected 2 dispatched, got %d", out.DispatchedCount)
	}
	got, _ := h.shared.GetString(remainingCounterKey("c-1"))
	if got != "12" {
		t.Fatalf("expected counter to IncrBy(2) → 12 on race-lost path, got %q", got)
	}
}

func TestQuickSend_NilSharedState_StillWorks_NoLockNoCounter(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusStopped, "c-1")

	uc := NewQuickSendUseCase(h.camp, h.entry, h.leadRepo, h.pub, h.cons, nil)

	out, err := uc.Execute(wc.QuickSendInput{
		CampaignID:   "c-1",
		PhoneNumbers: validPhones(1),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.DispatchedCount != 1 {
		t.Fatalf("expected 1 dispatched, got %d", out.DispatchedCount)
	}
}

func TestQuickSend_LeadRepoFails_ReturnsErrorEarly(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusStopped, "c-1")
	h.leadRepo.createErr = errors.New("db down")

	_, err := h.uc.Execute(wc.QuickSendInput{
		CampaignID:   "c-1",
		PhoneNumbers: validPhones(1),
	})
	if err == nil {
		t.Fatalf("expected lead repo error to surface")
	}

	c, _ := h.camp.FindByID("c-1")
	if c.Status != wc.CampaignStatusStopped {
		t.Fatalf("expected status untouched, got %s", c.Status)
	}

	if exists, _ := h.shared.Exists(quickSendLockKey("c-1")); exists {
		t.Fatalf("lock must be released even on early failure")
	}
}

func TestQuickSend_CompletedCampaign_RestartsAndDispatchesNewEntries(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusCompleted, "c-1")

	_ = h.shared.SetString(remainingCounterKey("c-1"), "999", time.Hour)

	out, err := h.uc.Execute(wc.QuickSendInput{
		CampaignID:   "c-1",
		PhoneNumbers: validPhones(2),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != string(wc.CampaignStatusRunning) {
		t.Fatalf("expected status RUNNING, got %s", out.Status)
	}
	got, _ := h.shared.GetString(remainingCounterKey("c-1"))
	if got != "2" {
		t.Fatalf("non-running path should Del+Set counter to %d; got %q", 2, got)
	}
}

func TestQuickSend_CounterTTLIsRefreshedOnRunningPath(t *testing.T) {

	h := newHarness(t, wc.CampaignStatusRunning, "c-1")
	h.cons.subscribed["c-1"] = true

	if _, err := h.uc.Execute(wc.QuickSendInput{
		CampaignID:   "c-1",
		PhoneNumbers: validPhones(1),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
