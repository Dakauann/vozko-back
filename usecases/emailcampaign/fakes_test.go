package emailcampaign_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/cache"
	"vozko/domain/emailaddress"
	"vozko/domain/emailcampaign"
	"vozko/domain/emailconsent"
	"vozko/domain/emaildesign"
	"vozko/domain/emailhygiene"
	"vozko/domain/emailsending"
	"vozko/domain/lead"
	"vozko/domain/messaging"
	"vozko/domain/notification"
	"vozko/domain/selection"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	balance_usecase "vozko/usecases/balance"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
	"vozko/usecases/campaignaudience/audiencetest"
	"vozko/usecases/emailhygiene/hygienetest"
)

var now = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

const ws = "ws-1"

type memCampaigns struct {
	mu    sync.Mutex
	rows  map[string]emailcampaign.Campaign
	seq   int
	claim map[string]bool
}

func (m *memCampaigns) Create(_ context.Context, c *emailcampaign.Campaign) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	if c.ID == "" {
		c.ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", m.seq)
	}
	c.CreatedAt, c.UpdatedAt = now, now
	m.rows[c.ID] = *c
	return nil
}

func (m *memCampaigns) Get(_ context.Context, workspaceID, id string) (*emailcampaign.Campaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok || c.WorkspaceID != workspaceID {
		return nil, emailcampaign.ErrCampaignNotFound
	}
	return &c, nil
}

func (m *memCampaigns) GetByID(_ context.Context, id string) (*emailcampaign.Campaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		return nil, emailcampaign.ErrCampaignNotFound
	}
	return &c, nil
}

func (m *memCampaigns) Update(_ context.Context, c *emailcampaign.Campaign, from emailcampaign.Status) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.rows[c.ID]
	if !ok {
		return emailcampaign.ErrCampaignNotFound
	}
	if !stored.Status.Editable() || stored.Status != from {
		return emailcampaign.ErrNotEditable
	}
	m.rows[c.ID] = *c
	return nil
}

func (m *memCampaigns) Delete(_ context.Context, workspaceID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok || c.WorkspaceID != workspaceID {
		return emailcampaign.ErrCampaignNotFound
	}
	if !c.Status.Deletable() {
		return emailcampaign.ErrNotEditable
	}
	delete(m.rows, id)
	return nil
}

func (m *memCampaigns) List(_ context.Context, workspaceID string, _ emailcampaign.ListQuery) ([]emailcampaign.Campaign, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []emailcampaign.Campaign
	for _, c := range m.rows {
		if c.WorkspaceID == workspaceID {
			out = append(out, c)
		}
	}
	return out, int64(len(out)), nil
}

func (m *memCampaigns) mutate(id string, from emailcampaign.Status, f func(*emailcampaign.Campaign)) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok || c.Status != from {
		return false
	}
	f(&c)
	c.UpdatedAt = now
	m.rows[id] = c
	return true
}

func (m *memCampaigns) SwapStatus(_ context.Context, id string, from, to emailcampaign.Status, reason emailcampaign.HoldReason) (bool, error) {
	return m.mutate(id, from, func(c *emailcampaign.Campaign) { c.Status, c.HoldReason = to, reason }), nil
}

func (m *memCampaigns) Schedule(_ context.Context, id string, from emailcampaign.Status, at time.Time, by string) (bool, error) {
	return m.mutate(id, from, func(c *emailcampaign.Campaign) {
		c.Status, c.ScheduledAt, c.LaunchedBy, c.HoldReason = emailcampaign.StatusScheduled, &at, by, ""
	}), nil
}

func (m *memCampaigns) MarkLaunched(_ context.Context, id string, from emailcampaign.Status, l emailcampaign.Launch) (bool, error) {
	return m.mutate(id, from, func(c *emailcampaign.Campaign) {
		c.Status, c.StartedAt, c.LaunchedBy, c.ScheduledAt, c.HoldReason = emailcampaign.StatusRunning, &l.StartedAt, l.LaunchedBy, nil, ""
	}), nil
}

func (m *memCampaigns) SetSettlement(_ context.Context, id string, s emailcampaign.RunSettlement) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok || c.Settlement != nil {
		return false, nil
	}
	c.Settlement = &s
	m.rows[id] = c
	return true, nil
}

func (m *memCampaigns) SetWinner(_ context.Context, id string, winner emailcampaign.Variant) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.rows[id]
	if c.ABTest == nil || c.ABTest.Winner != "" {
		return false, nil
	}
	ab := *c.ABTest
	ab.Winner = winner
	c.ABTest = &ab
	m.rows[id] = c
	return true, nil
}

func (m *memCampaigns) Complete(_ context.Context, id string, at time.Time) (bool, error) {
	return m.mutate(id, emailcampaign.StatusRunning, func(c *emailcampaign.Campaign) { c.Status, c.CompletedAt = emailcampaign.StatusCompleted, &at }), nil
}

func (m *memCampaigns) ListByStatus(_ context.Context, statuses ...emailcampaign.Status) ([]emailcampaign.Campaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []emailcampaign.Campaign
	for _, c := range m.rows {
		for _, s := range statuses {
			if c.Status == s {
				out = append(out, c)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *memCampaigns) ListScheduledDue(_ context.Context, at time.Time, _ int) ([]emailcampaign.Campaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []emailcampaign.Campaign
	for _, c := range m.rows {
		if c.Status == emailcampaign.StatusScheduled && c.ScheduledAt != nil && !c.ScheduledAt.After(at) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memCampaigns) ListAwaitingWinner(_ context.Context, at time.Time, _ int) ([]emailcampaign.Campaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []emailcampaign.Campaign
	for _, c := range m.rows {
		if c.Status.Sending() && c.ABTest != nil && c.ABTest.Winner == "" && c.StartedAt != nil && !c.ABTest.DecideAt(*c.StartedAt).After(at) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memCampaigns) ClaimScheduledStart(id string, due time.Time) (bool, error) {
	return m.mutate(id, emailcampaign.StatusScheduled, func(c *emailcampaign.Campaign) { c.ScheduledAt = nil }), nil
}

func (m *memCampaigns) Names(context.Context, string, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

type memRecipients struct {
	mu    sync.Mutex
	rows  map[string]emailcampaign.Recipient
	seq   int
	ended func(id string)
}

type memBatches struct {
	mu        sync.Mutex
	campaigns *memCampaigns
	recips    *memRecipients
	slots     map[string]emailcampaign.SlotCounters
	batches   map[string][]emailcampaign.ChargeBatch
	claims    map[string]time.Time
}

func newMemBatches(campaigns *memCampaigns, recips *memRecipients) *memBatches {
	b := &memBatches{campaigns: campaigns, recips: recips, slots: map[string]emailcampaign.SlotCounters{},
		batches: map[string][]emailcampaign.ChargeBatch{}, claims: map[string]time.Time{}}
	recips.ended = b.dropClaim
	return b
}

func (b *memBatches) BillingState(_ context.Context, campaignID string) (emailcampaign.BillingState, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return emailcampaign.BillingState{Slots: b.slots[campaignID], Batches: append([]emailcampaign.ChargeBatch(nil), b.batches[campaignID]...)}, nil
}

func (b *memBatches) OpenBatch(_ context.Context, campaignID string, next emailcampaign.ChargeBatch) (emailcampaign.ChargeBatch, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := emailcampaign.BillingState{Batches: b.batches[campaignID]}
	if state.OpenBatch() == nil && state.NextBatchNo() == next.No {
		b.batches[campaignID] = append(b.batches[campaignID], next)
		state.Batches = b.batches[campaignID]
	}
	if open := state.OpenBatch(); open != nil {
		return *open, nil
	}
	return emailcampaign.ChargeBatch{}, emailcampaign.ErrNoOpenBatch
}

func (b *memBatches) MarkBatchCharged(_ context.Context, campaignID string, batchNo int, at time.Time) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, batch := range b.batches[campaignID] {
		if batch.No == batchNo && !batch.Charged() {
			b.batches[campaignID][i].ChargedAt = &at
			slots := b.slots[campaignID]
			slots.Paid += batch.Units
			b.slots[campaignID] = slots
			return true, nil
		}
	}
	return false, nil
}

func (b *memBatches) MarkBatchRefunded(_ context.Context, campaignID string, batchNo int, units int64, at time.Time) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, batch := range b.batches[campaignID] {
		if batch.No == batchNo && !batch.Refunded() {
			b.batches[campaignID][i].RefundedUnits, b.batches[campaignID][i].RefundedAt = units, &at
			return true, nil
		}
	}
	return false, nil
}

func (b *memBatches) ClaimSlot(ctx context.Context, campaignID, recipientID string, at time.Time) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.campaigns.GetByID(ctx, campaignID)
	if err != nil || !c.Status.Sending() {
		return false, err
	}
	rc, err := b.recips.Get(ctx, recipientID)
	if err != nil || rc.CampaignID != campaignID || rc.Status != emailcampaign.RecipientPending {
		return false, nil
	}
	if _, held := b.claims[recipientID]; held {
		b.claims[recipientID] = at
		return true, nil
	}
	slots := b.slots[campaignID]
	if !slots.HasFree() {
		return false, nil
	}
	slots.Used++
	b.slots[campaignID] = slots
	b.claims[recipientID] = at
	return true, nil
}

func (b *memBatches) ReleaseSlot(ctx context.Context, campaignID, recipientID string) (bool, error) {
	rc, err := b.recips.Get(ctx, recipientID)
	if err != nil || rc.CampaignID != campaignID || rc.Status != emailcampaign.RecipientPending {
		return false, nil
	}
	return b.release(campaignID, recipientID), nil
}

func (b *memBatches) dropClaim(recipientID string) {
	rc, err := b.recips.Get(context.Background(), recipientID)
	if err == nil {
		b.release(rc.CampaignID, recipientID)
	}
}

func (b *memBatches) release(campaignID, recipientID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, held := b.claims[recipientID]; !held {
		return false
	}
	delete(b.claims, recipientID)
	slots := b.slots[campaignID]
	slots.Used = max(slots.Used-1, 0)
	b.slots[campaignID] = slots
	return true
}

func (b *memBatches) FreshClaims(ctx context.Context, campaignID string, since time.Time) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var n int64
	for id, at := range b.claims {
		rc, err := b.recips.Get(ctx, id)
		if err == nil && rc.CampaignID == campaignID && rc.Status == emailcampaign.RecipientPending && at.After(since) {
			n++
		}
	}
	return n, nil
}

func (b *memBatches) ListUnsettled(ctx context.Context, _ int) ([]emailcampaign.Campaign, error) {
	finished, err := b.campaigns.ListByStatus(ctx, emailcampaign.StatusStopped, emailcampaign.StatusCompleted)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []emailcampaign.Campaign
	for _, c := range finished {
		if c.Settlement == nil && len(b.batches[c.ID]) > 0 {
			out = append(out, c)
		}
	}
	return out, nil
}

func (b *memBatches) state(campaignID string) emailcampaign.BillingState {
	s, _ := b.BillingState(context.Background(), campaignID)
	return s
}

type memLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (l *memLocks) Acquire(_ context.Context, key string) (func() (bool, error), error) {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = map[string]*sync.Mutex{}
	}
	lock, ok := l.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		l.locks[key] = lock
	}
	l.mu.Unlock()
	lock.Lock()
	return func() (bool, error) { lock.Unlock(); return true, nil }, nil
}

func (m *memRecipients) Replace(_ context.Context, workspaceID, campaignID string, rs []emailcampaign.Recipient) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, r := range m.rows {
		if r.CampaignID == campaignID {
			delete(m.rows, id)
		}
	}
	for _, r := range rs {
		m.seq++
		r.ID = fmt.Sprintf("10000000-0000-4000-8000-%012d", m.seq)
		r.WorkspaceID, r.CampaignID = workspaceID, campaignID
		m.rows[r.ID] = r
	}
	return nil
}

func (m *memRecipients) Get(_ context.Context, id string) (*emailcampaign.Recipient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return nil, emailcampaign.ErrRecipientNotFound
	}
	return &r, nil
}

func (m *memRecipients) settle(id string, f func(*emailcampaign.Recipient)) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok || r.Status != emailcampaign.RecipientPending {
		return false, nil
	}
	f(&r)
	m.rows[id] = r
	return true, nil
}

func (m *memRecipients) MarkSent(_ context.Context, id, messageID string, at time.Time) (bool, error) {
	return m.settle(id, func(r *emailcampaign.Recipient) {
		r.Status, r.MessageID, r.SentAt = emailcampaign.RecipientSent, messageID, &at
	})
}

func (m *memRecipients) endUnsent(id string, f func(*emailcampaign.Recipient)) (bool, error) {
	ok, err := m.settle(id, f)
	if ok && m.ended != nil {
		m.ended(id)
	}
	return ok, err
}

func (m *memRecipients) MarkSkipped(_ context.Context, id string, reason emailcampaign.SkipReason) (bool, error) {
	return m.endUnsent(id, func(r *emailcampaign.Recipient) { r.Status, r.SkipReason = emailcampaign.RecipientSkipped, reason })
}

func (m *memRecipients) MarkFailed(_ context.Context, id, code string) (bool, error) {
	return m.endUnsent(id, func(r *emailcampaign.Recipient) { r.Status, r.FailureCode = emailcampaign.RecipientFailed, code })
}

func (m *memRecipients) Counts(_ context.Context, campaignID string) (emailcampaign.RecipientCounts, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	counts := emailcampaign.RecipientCounts{Skipped: map[emailcampaign.SkipReason]int64{}}
	for _, r := range m.rows {
		if r.CampaignID != campaignID {
			continue
		}
		counts.Total++
		switch r.Status {
		case emailcampaign.RecipientPending:
			counts.Pending++
		case emailcampaign.RecipientSent:
			counts.Sent++
		case emailcampaign.RecipientFailed:
			counts.Failed++
		case emailcampaign.RecipientSkipped:
			counts.Skipped[r.SkipReason]++
		}
	}
	return counts, nil
}

func (m *memRecipients) PendingIDs(_ context.Context, campaignID string, variants []emailcampaign.Variant, after string, limit int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []string
	for _, r := range m.rows {
		if r.CampaignID != campaignID || r.Status != emailcampaign.RecipientPending || r.ID <= after {
			continue
		}
		for _, v := range variants {
			if r.Variant == v {
				ids = append(ids, r.ID)
			}
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

func (m *memRecipients) List(context.Context, string, string, emailcampaign.RecipientFilter, int, int) ([]emailcampaign.RecipientRow, int64, error) {
	return nil, 0, nil
}

func (m *memRecipients) byAddress(campaignID string) map[emailaddress.Address]emailcampaign.Recipient {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[emailaddress.Address]emailcampaign.Recipient{}
	for _, r := range m.rows {
		if r.CampaignID == campaignID {
			out[r.Address] = r
		}
	}
	return out
}

type stubMetrics struct {
	variants      map[emailcampaign.Variant]emailcampaign.VariantResult
	overviewSince time.Time
	summaries     map[string]emailcampaign.Tally
	summaryCalls  int
}

func (s *stubMetrics) Tally(context.Context, string, string) (emailcampaign.Tally, error) {
	return emailcampaign.Tally{Sent: 10, Delivered: 8, Opens: 4}, nil
}
func (s *stubMetrics) Summaries(_ context.Context, _ string, ids []string) (map[string]emailcampaign.Tally, error) {
	s.summaryCalls++
	out := map[string]emailcampaign.Tally{}
	for _, id := range ids {
		if t, ok := s.summaries[id]; ok {
			out[id] = t
		}
	}
	return out, nil
}
func (s *stubMetrics) VariantResults(context.Context, string, string) (map[emailcampaign.Variant]emailcampaign.VariantResult, error) {
	return s.variants, nil
}
func (s *stubMetrics) Timeline(context.Context, string, string, emailcampaign.Bucket) ([]emailcampaign.TimelinePoint, error) {
	return nil, nil
}
func (s *stubMetrics) Links(context.Context, string, string, int) ([]emailcampaign.LinkStat, error) {
	return nil, nil
}
func (s *stubMetrics) Domains(context.Context, string, string) ([]emailcampaign.DomainCount, error) {
	return []emailcampaign.DomainCount{{Domain: "gmail.com", Sent: 1}}, nil
}
func (s *stubMetrics) Overview(_ context.Context, _ string, since time.Time, _ int) (emailcampaign.OverviewData, error) {
	s.overviewSince = since
	return emailcampaign.OverviewData{Totals: emailcampaign.Tally{Sent: 100, Delivered: 90, HardBounces: 1}}, nil
}

type memDesigns struct {
	rows map[string]emailcampaign.Design
}

func (m *memDesigns) Create(_ context.Context, d *emailcampaign.Design) error {
	d.ID = "20000000-0000-4000-8000-000000000001"
	m.rows[d.ID] = *d
	return nil
}
func (m *memDesigns) Get(_ context.Context, workspaceID, id string) (*emailcampaign.Design, error) {
	d, ok := m.rows[id]
	if !ok || d.WorkspaceID != workspaceID {
		return nil, emailcampaign.ErrDesignNotFound
	}
	return &d, nil
}
func (m *memDesigns) Update(_ context.Context, d *emailcampaign.Design) error {
	m.rows[d.ID] = *d
	return nil
}
func (m *memDesigns) Delete(_ context.Context, _, id string) error {
	delete(m.rows, id)
	return nil
}
func (m *memDesigns) List(context.Context, string, emailcampaign.DesignKind, int, int) ([]emailcampaign.Design, int64, error) {
	return nil, 0, nil
}

type stubSelection struct{ ids []string }

func (s stubSelection) Count(context.Context, selection.Scope, selection.Selection) (int, error) {
	return len(s.ids), nil
}

func (s stubSelection) Resolve(_ context.Context, _ selection.Scope, _ selection.Selection, after string, limit int) ([]selection.Ref, error) {
	var out []selection.Ref
	for _, id := range s.ids {
		if id > after && len(out) < limit {
			out = append(out, selection.Ref{ID: id, Type: lead.SelectionRefType})
		}
	}
	return out, nil
}

type stubFacts map[string]lead.EmailFacts

func (s stubFacts) EmailFacts(_ context.Context, _ string, ids []string) (map[string]lead.EmailFacts, error) {
	out := map[string]lead.EmailFacts{}
	for _, id := range ids {
		if f, ok := s[id]; ok {
			out[id] = f
		}
	}
	return out, nil
}

type stubLeads struct{}

func (stubLeads) FindByEmail(string, string) (*lead.Lead, error) { return nil, lead.ErrLeadNotFound }

type memSubscriptions struct {
	mu   sync.Mutex
	rows map[emailaddress.Address]emailconsent.Subscription
}

func (m *memSubscriptions) Get(_ context.Context, _ string, a emailaddress.Address) (*emailconsent.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.rows[a]
	if !ok {
		return nil, emailconsent.ErrSubscriptionNotFound
	}
	return &s, nil
}
func (m *memSubscriptions) Find(_ context.Context, _ string, as []emailaddress.Address) (map[emailaddress.Address]emailconsent.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[emailaddress.Address]emailconsent.Subscription{}
	for _, a := range as {
		if s, ok := m.rows[a]; ok {
			out[a] = s
		}
	}
	return out, nil
}
func (m *memSubscriptions) Save(_ context.Context, s emailconsent.Subscription) (emailconsent.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[s.Address] = s
	return s, nil
}
func (m *memSubscriptions) SaveMany(ctx context.Context, subs []emailconsent.Subscription) error {
	for _, s := range subs {
		_, _ = m.Save(ctx, s)
	}
	return nil
}

type stubHygiene struct {
	mu          sync.Mutex
	neverMailed bool
	supp        map[emailaddress.Address]emailsending.SuppressReason
	standing    map[emailaddress.Address]emailhygiene.Standing
	facts       map[emailaddress.Address]emailhygiene.Facts
	reads       int
	requested   []string
}

func newStubHygiene() *stubHygiene {
	return &stubHygiene{supp: map[emailaddress.Address]emailsending.SuppressReason{}, standing: map[emailaddress.Address]emailhygiene.Standing{}, facts: map[emailaddress.Address]emailhygiene.Facts{}}
}

func (s *stubHygiene) Standing(_ context.Context, _ string, as []emailaddress.Address) (map[emailaddress.Address]emailhygiene.Standing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	mailed := now.AddDate(0, -1, 0)
	out := map[emailaddress.Address]emailhygiene.Standing{}
	for _, a := range as {
		st, ok := s.standing[a]
		if !ok {
			st = emailhygiene.Standing{Domain: emailhygiene.DomainAccepts, Activity: emailhygiene.Activity{Known: true, FirstDeliveredAt: &mailed}}
			if s.neverMailed {
				st.Activity = emailhygiene.Activity{}
			}
		}
		if r, ok := s.supp[a]; ok {
			st.Suppression = r
		}
		out[a] = st
	}
	return out, nil
}

func (s *stubHygiene) Facts(a emailaddress.Address) emailhygiene.Facts {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.facts[a]
}

func (s *stubHygiene) RequestStale(_ context.Context, standing map[emailaddress.Address]emailhygiene.Standing) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for a, st := range standing {
		if st.DomainStale {
			s.requested = append(s.requested, a.Domain())
		}
	}
	return nil
}

type gatewayCall struct {
	req     emailsending.SendRequest
	content emailsending.Content
}

type journal struct {
	mu      sync.Mutex
	entries []string
}

func (j *journal) add(entry string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries = append(j.entries, entry)
}

func (j *journal) all() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.entries...)
}

type fakeGateway struct {
	mu        sync.Mutex
	calls     []gatewayCall
	err       error
	byAddress map[emailaddress.Address]error
	seq       int
	journal   *journal
	onSend    func(sentBefore int)
}

func (g *fakeGateway) Send(_ context.Context, req emailsending.SendRequest, compose emailsending.ComposeFunc) (*emailsending.Message, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.journal != nil {
		g.journal.add("send " + req.To.String())
	}
	if g.err != nil {
		return nil, g.err
	}
	if err := g.byAddress[req.To]; err != nil {
		return nil, err
	}
	content, err := compose(emailsending.Envelope{UnsubscribeURL: fakeUnsubscribeURL})
	if err != nil {
		return nil, err
	}
	if g.onSend != nil {
		g.onSend(len(g.calls))
	}
	g.seq++
	g.calls = append(g.calls, gatewayCall{req: req, content: content})
	return &emailsending.Message{ID: "msg-" + strconv.Itoa(g.seq), Address: req.To}, nil
}

const fakeUnsubscribeURL = "https://mail.example.com/u/token"

func (g *fakeGateway) Envelope(_ context.Context, req emailsending.SendRequest, sent emailsending.Message) (emailsending.Envelope, error) {
	if sent.WorkspaceID != req.WorkspaceID {
		return emailsending.Envelope{}, emailsending.ErrMessageNotFound
	}
	return emailsending.Envelope{MessageID: sent.ID, UnsubscribeURL: fakeUnsubscribeURL}, nil
}

type stubSenders struct {
	missing bool
	domain  emailsending.SendingDomain
}

func (s stubSenders) Get(context.Context, string, string) (*emailsending.Sender, error) {
	if s.missing {
		return nil, emailsending.ErrSenderNotFound
	}
	return &emailsending.Sender{ID: "sender-1", DomainID: "domain-1", LocalPart: "news"}, nil
}
func (s stubSenders) GetDefault(ctx context.Context, w string) (*emailsending.Sender, error) {
	return s.Get(ctx, w, "")
}

type stubDomains struct{ status emailsending.DomainStatus }

func (s stubDomains) Get(context.Context, string, string) (*emailsending.SendingDomain, error) {
	return &emailsending.SendingDomain{ID: "domain-1", Domain: "loja.com.br", Kind: emailsending.DomainOwn, Status: s.status}, nil
}

type stubAccounts struct {
	status emailsending.AccountStatus
	reason string
}

func (s stubAccounts) GetByWorkspace(context.Context, string) (*emailsending.SenderAccount, error) {
	return &emailsending.SenderAccount{TenantName: "ws-1", Mode: emailsending.ModeOwnDomain, Status: s.status, StatusReason: s.reason}, nil
}

type stubRates struct {
	rate int64
	err  error
}

func (s *stubRates) Execute() (*workspace_pricing.PricingItem, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &workspace_pricing.PricingItem{PriceMicros: s.rate, UpdatedAt: now.Add(-time.Hour)}, nil
}

type refundCall struct {
	key   string
	quote balance_usecase.EmailQuote
}

type batchRefundCall struct {
	key        string
	units      int64
	unitMicros int64
}

type fakeBilling struct {
	mu             sync.Mutex
	unit           int64
	balance        int64
	batchCharges   map[string]balance_usecase.EmailQuote
	batchOrder     []string
	batchRefunds   []batchRefundCall
	messageCharges []string
	refunds        []refundCall
	refundErr      error
	journal        *journal
}

func (b *fakeBilling) ChargeMessage(_ context.Context, _ string, key string) (balance_usecase.EmailQuote, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	q := b.quote(1)
	if q.TotalMicros > b.balance {
		return balance_usecase.EmailQuote{}, &balance_usecase.InsufficientEmailBalanceError{MissingMicros: q.TotalMicros - b.balance}
	}
	b.balance -= q.TotalMicros
	b.messageCharges = append(b.messageCharges, key)
	if b.journal != nil {
		b.journal.add("charge " + key)
	}
	return q, nil
}

func (b *fakeBilling) RefundMessage(_ context.Context, _ string, key string, charge balance_usecase.EmailQuote) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.refundErr != nil {
		return b.refundErr
	}
	b.balance += charge.TotalMicros
	b.refunds = append(b.refunds, refundCall{key: key, quote: charge})
	if b.journal != nil {
		b.journal.add("refund " + key)
	}
	return nil
}

type fakeReactivation struct {
	err     error
	calls   int
	journal *journal
}

func (r *fakeReactivation) Reactivate(_ context.Context, workspaceID string) error {
	r.calls++
	if r.journal != nil {
		r.journal.add("reactivate " + workspaceID)
	}
	return r.err
}

func (b *fakeBilling) quote(n int64) balance_usecase.EmailQuote {
	return balance_usecase.EmailQuote{Units: n, UnitMicros: b.unit, UnitCostMicros: 1, TotalMicros: b.unit * n}
}
func (b *fakeBilling) QuoteRun(_ context.Context, _ string, n int64) (balance_usecase.EmailQuote, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.quote(n), nil
}
func batchKey(campaignID string, batchNo int) string {
	return campaignID + ":" + strconv.Itoa(batchNo)
}

func (b *fakeBilling) ChargeBatch(_ context.Context, _ string, campaignID string, batchNo int, q balance_usecase.EmailQuote) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := batchKey(campaignID, batchNo)
	if _, done := b.batchCharges[key]; done {
		return nil
	}
	total := q.Units * q.UnitMicros
	if total > b.balance {
		return &balance_usecase.InsufficientEmailBalanceError{MissingMicros: total - b.balance}
	}
	b.balance -= total
	b.batchCharges[key] = q
	b.batchOrder = append(b.batchOrder, key)
	if b.journal != nil {
		b.journal.add("charge batch " + key)
	}
	return nil
}

func (b *fakeBilling) BatchCharged(_ context.Context, campaignID string, batchNo int) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, done := b.batchCharges[batchKey(campaignID, batchNo)]
	return done, nil
}

func (b *fakeBilling) RefundBatch(_ context.Context, _ string, campaignID string, batchNo int, q balance_usecase.EmailQuote, units int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := batchKey(campaignID, batchNo)
	for _, r := range b.batchRefunds {
		if r.key == key {
			return nil
		}
	}
	if _, done := b.batchCharges[key]; !done || units > q.Units {
		return balance.ErrRefundExceedsCharge
	}
	b.balance += units * q.UnitMicros
	b.batchRefunds = append(b.batchRefunds, batchRefundCall{key: key, units: units, unitMicros: q.UnitMicros})
	return nil
}

func (b *fakeBilling) charges() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.batchOrder...)
}

func (b *fakeBilling) wallet() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.balance
}

func (b *fakeBilling) setUnit(unit int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.unit = unit
}

func (b *fakeBilling) topUp(micros int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.balance += micros
}

type stubWallet struct{ amount *int64 }

func (w stubWallet) GetByWorkspaceID(string) (*balance.Balance, error) {
	return &balance.Balance{Amount: *w.amount}, nil
}

type fakePub struct {
	mu        sync.Mutex
	published []string
	delayed   int
}

func (p *fakePub) Publish(topic string, msg []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.published = append(p.published, string(msg))
	return nil
}
func (p *fakePub) PublishWithDelay(string, []byte, time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.delayed++
	return nil
}
func (p *fakePub) ValidateConnection() error { return nil }

func (p *fakePub) entryIDs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var ids []string
	for _, raw := range p.published {
		var m struct {
			EntryID string `json:"entryId"`
		}
		_ = json.Unmarshal([]byte(raw), &m)
		ids = append(ids, m.EntryID)
	}
	return ids
}

type fakeSub struct {
	mu         sync.Mutex
	subscribed map[string]bool
	deleted    []string
}

func (s *fakeSub) Subscribe(topic string, _ func([]byte, messaging.MessageAck)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscribed[topic] = true
	return nil
}
func (s *fakeSub) DeleteQueue(topic string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, topic)
	return nil
}
func (s *fakeSub) ValidateConnection() error          { return nil }
func (s *fakeSub) GetQueueLength(string) (int, error) { return 0, nil }

type fakeShared struct {
	cache.SharedState
	mu   sync.Mutex
	vals map[string]string
}

func (f *fakeShared) SetString(key, value string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vals[key] = value
	return nil
}
func (f *fakeShared) GetString(key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.vals[key], nil
}
func (f *fakeShared) Del(keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.vals, k)
	}
	return nil
}
func (f *fakeShared) Exists(key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.vals[key]
	return ok, nil
}
func (f *fakeShared) Decr(key string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, _ := strconv.ParseInt(f.vals[key], 10, 64)
	n--
	f.vals[key] = strconv.FormatInt(n, 10)
	return n, nil
}

type passMemo struct{}

func (passMemo) Remember(ctx context.Context, _ string, _ time.Duration, compute func(context.Context) ([]byte, error)) ([]byte, error) {
	return compute(ctx)
}

type recordingPublisher struct {
	mu       sync.Mutex
	drafts   []notification.Draft
	resolved []string
}

func (p *recordingPublisher) Resolve(_ context.Context, _ string, keys ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resolved = append(p.resolved, keys...)
	return nil
}

func (p *recordingPublisher) Publish(_ context.Context, d notification.Draft) (notification.Record, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.drafts = append(p.drafts, d)
	return notification.Record{}, nil
}

type world struct {
	t         *testing.T
	svc       *Service
	campaigns *memCampaigns
	recips    *memRecipients
	batches   *memBatches
	metrics   *stubMetrics
	subs      *memSubscriptions
	gateway   *fakeGateway
	billing   *fakeBilling
	pub       *fakePub
	sub       *fakeSub
	notes     *recordingPublisher
	facts     stubFacts
	selection *stubSelection
	hygiene   *stubHygiene
	outcomes  *stubOutcomes
	wallet    *int64
	senders   *stubSenders
	accounts  *stubAccounts
	rates     *stubRates
	revival   *fakeReactivation
	journal   *journal
	aud       *campaignaudience_usecase.Service
	lists     *audiencetest.Lists
	created   *createdEmailLeads
}

func newWorld(t *testing.T) *world {
	t.Helper()
	amount := int64(1_000_000_000)
	log := &journal{}
	campaigns := &memCampaigns{rows: map[string]emailcampaign.Campaign{}}
	recips := &memRecipients{rows: map[string]emailcampaign.Recipient{}}
	w := &world{
		t:         t,
		campaigns: campaigns,
		recips:    recips,
		batches:   newMemBatches(campaigns, recips),
		metrics:   &stubMetrics{},
		subs:      &memSubscriptions{rows: map[emailaddress.Address]emailconsent.Subscription{}},
		gateway:   &fakeGateway{journal: log},
		billing:   &fakeBilling{unit: 583, balance: amount, batchCharges: map[string]balance_usecase.EmailQuote{}, journal: log},
		revival:   &fakeReactivation{journal: log},
		journal:   log,
		pub:       &fakePub{},
		sub:       &fakeSub{subscribed: map[string]bool{}},
		notes:     &recordingPublisher{},
		facts:     stubFacts{},
		selection: &stubSelection{},
		hygiene:   newStubHygiene(),
		outcomes:  &stubOutcomes{runs: map[string]emailsending.RunOutcome{}},
		wallet:    &amount,
		senders:   &stubSenders{},
		accounts:  &stubAccounts{status: emailsending.AccountActive},
		rates:     &stubRates{rate: 5_500_000},
	}
	svc, err := New(Deps{
		Campaigns: w.campaigns, Recipients: w.recips, Batches: w.batches, Locks: &memLocks{}, Metrics: w.metrics, Designs: &memDesigns{rows: map[string]emailcampaign.Design{}},
		Selection: selectionProxy{w}, LeadFacts: w.facts, Leads: stubLeads{}, Subscriptions: w.subs, Hygiene: w.hygiene, Outcomes: w.outcomes,
		Gateway: w.gateway, Senders: sendersProxy{w}, Domains: stubDomains{status: emailsending.DomainVerified},
		Accounts: accountsProxy{w}, Reactivation: w.revival, Billing: w.billing, Wallet: stubWallet{amount: w.wallet}, Rates: w.rates,
		QueuePub: w.pub, QueueSub: w.sub, Shared: &fakeShared{vals: map[string]string{}}, Memo: passMemo{}, Notifications: w.notes,
		PublicBaseURL: "https://api.example.com/",
		Now:           func() time.Time { return now },
		Logf:          func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.svc = svc
	w.lists, w.created = audiencetest.NewLists(), &createdEmailLeads{w: w}
	aud, err := campaignaudience_usecase.New(campaignaudience_usecase.Deps{
		Selections: selectionCounts{w}, Lists: w.lists, Sheets: sheetText{}, Phones: noPhones{}, Emails: knownEmails{w},
		EmailLeads: w.created, EmailsSeen: &hygienetest.SeenEmails{}, Definitions: noDefinitions{}, Gate: freeGate{}, Flight: audiencetest.Flight(audiencetest.NewState()), Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	aud.AddChannel(svc.AudienceChannel())
	svc.SetAudience(aud)
	w.aud = aud
	return w
}

type selectionProxy struct{ w *world }

func (p selectionProxy) Count(ctx context.Context, s selection.Scope, sel selection.Selection) (int, error) {
	return p.w.selection.Count(ctx, s, sel)
}
func (p selectionProxy) Resolve(ctx context.Context, s selection.Scope, sel selection.Selection, after string, limit int) ([]selection.Ref, error) {
	return p.w.selection.Resolve(ctx, s, sel, after, limit)
}

type sendersProxy struct{ w *world }

func (p sendersProxy) Get(ctx context.Context, ws, id string) (*emailsending.Sender, error) {
	return p.w.senders.Get(ctx, ws, id)
}
func (p sendersProxy) GetDefault(ctx context.Context, ws string) (*emailsending.Sender, error) {
	return p.w.senders.GetDefault(ctx, ws)
}

type accountsProxy struct{ w *world }

func (p accountsProxy) GetByWorkspace(ctx context.Context, ws string) (*emailsending.SenderAccount, error) {
	return p.w.accounts.GetByWorkspace(ctx, ws)
}

func (w *world) actor() Actor {
	return Actor{UserID: "user-1", WorkspaceID: ws}
}

type person struct {
	email        string
	subscribed   bool
	unsubscribed bool
	optedOut     bool
	blocked      bool
	suppressed   bool
}

func (w *world) audience(people ...person) {
	w.t.Helper()
	w.selection.ids = nil
	for i, p := range people {
		id := fmt.Sprintf("30000000-0000-4000-8000-%012d", i+1)
		w.selection.ids = append(w.selection.ids, id)
		w.facts[id] = lead.EmailFacts{ID: id, Name: "Pessoa " + strconv.Itoa(i+1), Email: p.email, OptedOut: p.optedOut, Blocked: p.blocked}
		address, err := emailaddress.Normalize(p.email)
		if err != nil {
			continue
		}
		if p.subscribed {
			sub, err := emailconsent.NewSubscription(emailconsent.Consent{WorkspaceID: ws, Address: p.email, Source: emailconsent.SourceImport, At: now}, now)
			if err != nil {
				w.t.Fatal(err)
			}
			w.subs.rows[address] = sub
		}
		if p.unsubscribed {
			sub, err := emailconsent.NewUnsubscribed(ws, p.email, id, emailconsent.UnsubscribeLink, now)
			if err != nil {
				w.t.Fatal(err)
			}
			w.subs.rows[address] = sub
		}
		if p.suppressed {
			w.hygiene.supp[address] = emailsending.SuppressHardBounce
		}
	}
}

func readyDocument() emaildesign.Document {
	return emaildesign.Document{
		Version: emaildesign.CurrentVersion,
		Blocks: []emaildesign.Block{
			{ID: "h1", Kind: emaildesign.KindHeading, Heading: &emaildesign.Heading{Text: "Olá {{first_name|cliente}}", Level: 1}},
			{ID: "b1", Kind: emaildesign.KindButton, Button: &emaildesign.Button{Label: "Comprar", Href: "https://loja.com.br/oferta"}},
			{ID: "f1", Kind: emaildesign.KindFooter, Footer: &emaildesign.Footer{CompanyName: "Loja", Address: "Rua A, 1"}},
		},
	}
}

func (w *world) draft(ab *emailcampaign.ABTest) emailcampaign.Campaign {
	w.t.Helper()
	sel := selection.Selection{Mode: selection.ModeEveryone}
	c, err := w.svc.Create(context.Background(), w.actor(), emailcampaign.Draft{
		Name: "Black Friday", Subject: "Oferta para {{first_name|você}}", Document: readyDocument(), Selection: &sel, ABTest: ab,
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return *c
}

func (w *world) campaign(id string) emailcampaign.Campaign {
	w.t.Helper()
	c, err := w.campaigns.GetByID(context.Background(), id)
	if err != nil {
		w.t.Fatal(err)
	}
	return *c
}

func (m *memCampaigns) SetTestBatch(_ context.Context, id string, from emailcampaign.TestBatchState, tb emailcampaign.TestBatch) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	current := emailcampaign.TestBatchNone
	if ok && c.TestBatch != nil {
		current = c.TestBatch.State
	}
	if !ok || current != from {
		return false, nil
	}
	copied := tb
	c.TestBatch = &copied
	m.rows[id] = c
	return true, nil
}

func (m *memCampaigns) ListTesting(_ context.Context, _ int) ([]emailcampaign.Campaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []emailcampaign.Campaign
	for _, c := range m.rows {
		if c.TestBatch.Holding() && c.Status.Sending() {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memRecipients) PendingThrough(_ context.Context, campaignID, lastID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, r := range m.rows {
		if r.CampaignID == campaignID && r.Status == emailcampaign.RecipientPending && r.ID <= lastID {
			n++
		}
	}
	return n, nil
}

type stubOutcomes struct {
	mu   sync.Mutex
	runs map[string]emailsending.RunOutcome
}

func (s *stubOutcomes) set(runID string, o emailsending.RunOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[runID] = o
}

func (s *stubOutcomes) RunOutcome(_ context.Context, _ emailsending.RunKind, runID string) (emailsending.RunOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[runID], nil
}
