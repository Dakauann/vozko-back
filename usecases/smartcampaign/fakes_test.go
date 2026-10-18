package smartcampaign_usecase

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"vozko/domain/balance"
	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/usecases/campaignqueue"
)

var errBroken = errors.New("broken on purpose")

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }

type fakeEntry struct {
	id     string
	status wce.SendStatus
	wave   int
	code   int
}

type store struct {
	mu        sync.Mutex
	campaigns map[string]*wc.Campaign
	entries   map[string][]*fakeEntry
	policies  map[string]sc.Policy
	states    map[string]sc.State
	waves     map[string][]sc.Wave
	stateErr  error
}

func newStore() *store {
	return &store{
		campaigns: map[string]*wc.Campaign{},
		entries:   map[string][]*fakeEntry{},
		policies:  map[string]sc.Policy{},
		states:    map[string]sc.State{},
		waves:     map[string][]sc.Wave{},
	}
}

func (s *store) addCampaign(id string, contacts int, policy *sc.Policy) *wc.Campaign {
	c := &wc.Campaign{ID: id, WorkspaceID: "ws", Name: "Promo " + id, TemplateID: "tpl-" + id, BusinessPhoneID: "phone-" + id, Status: campaign.StatusStopped, TemplateCategory: "UTILITY"}
	s.campaigns[id] = c
	for i := 0; i < contacts; i++ {
		s.entries[id] = append(s.entries[id], &fakeEntry{id: fmt.Sprintf("%s-%05d", id, i), status: wce.SendStatusPending})
	}
	if policy != nil {
		s.policies[id] = *policy
	}
	return c
}

func (s *store) FindByID(id string) (*wc.Campaign, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.campaigns[id]
	if !ok {
		return nil, wc.ErrCampaignNotFound
	}
	copied := *c
	return &copied, nil
}

func (s *store) UpdateStatus(id string, status wc.Status, allowed ...wc.Status) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.campaigns[id]
	if len(allowed) > 0 {
		match := false
		for _, a := range allowed {
			match = match || c.Status == a
		}
		if !match {
			return false, nil
		}
	}
	c.Status = status
	return true, nil
}

func (s *store) CountByStatus(id string) (*wce.StatusCounts, error) {
	counts := &wce.StatusCounts{}
	for _, e := range s.entries[id] {
		counts.Total++
		if e.status == wce.SendStatusPending {
			counts.Pending++
		}
	}
	return counts, nil
}

func (s *store) FindPolicy(id string) (*sc.Policy, error) {
	p, ok := s.policies[id]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (s *store) SavePolicy(id string, p sc.Policy) error {
	s.policies[id] = p
	return nil
}

func (s *store) DeletePolicy(id string) error {
	delete(s.policies, id)
	return nil
}

func (s *store) FindState(id string) (sc.State, error) {
	if st, ok := s.states[id]; ok {
		return st, nil
	}
	return sc.State{CampaignID: id}, nil
}

func (s *store) SaveState(st sc.State) error {
	if s.stateErr != nil {
		return s.stateErr
	}
	s.states[st.CampaignID] = st
	return nil
}

func (s *store) DueCampaigns(now time.Time, _ int) ([]string, error) {
	var due []string
	for id, st := range s.states {
		if _, smart := s.policies[id]; smart && s.campaigns[id].Status == campaign.StatusRunning && !st.DueAt.IsZero() && !st.DueAt.After(now) {
			due = append(due, id)
		}
	}
	sort.Strings(due)
	return due, nil
}

func (s *store) RunningUsing(templateID, phoneID string) ([]string, error) {
	var ids []string
	for id, c := range s.campaigns {
		if _, smart := s.policies[id]; !smart || c.Status != campaign.StatusRunning {
			continue
		}
		if (templateID != "" && c.TemplateID == templateID) || (phoneID != "" && c.BusinessPhoneID == phoneID) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *store) CurrentWave(id string) (*sc.Wave, error) {
	list := s.waves[id]
	if len(list) == 0 {
		return nil, nil
	}
	w := list[len(list)-1]
	return &w, nil
}

func (s *store) ListWaves(id string) ([]sc.Wave, error) {
	return append([]sc.Wave(nil), s.waves[id]...), nil
}

func (s *store) ClaimWave(id string, number, size int, at time.Time) (sc.Wave, error) {
	if number <= len(s.waves[id]) {
		return sc.Wave{}, sc.ErrAlreadyReleased
	}
	claimed := 0
	for _, e := range s.entries[id] {
		if claimed == size {
			break
		}
		if e.status == wce.SendStatusPending && e.wave == 0 {
			e.wave = number
			claimed++
		}
	}
	if claimed == 0 {
		return sc.Wave{}, sc.ErrNothingToClaim
	}
	w := sc.Release(id, number, claimed, at)
	s.waves[id] = append(s.waves[id], w)
	return w, nil
}

func (s *store) SaveWave(w sc.Wave) error {
	s.waves[w.CampaignID][w.Number-1] = w
	return nil
}

func (s *store) ReturnToPool(id string, number int) (int, error) {
	returned := 0
	for _, e := range s.entries[id] {
		if e.wave == number && e.status == wce.SendStatusPending {
			e.wave = 0
			returned++
		}
	}
	return returned, nil
}

func (s *store) ReleasedSince(id string, since time.Time) (int, error) {
	n := 0
	for _, w := range s.waves[id] {
		if !w.ReleasedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

func (s *store) PendingInWave(id string, number int) (int, error) {
	n := 0
	for _, e := range s.entries[id] {
		if e.wave == number && e.status == wce.SendStatusPending {
			n++
		}
	}
	return n, nil
}

func (s *store) WaveOutcome(id string, number int) (sc.Outcome, error) {
	o := sc.Outcome{Failures: map[int]int{}}
	for _, e := range s.entries[id] {
		if e.wave != number || e.status == wce.SendStatusPending {
			continue
		}
		o.Processed++
		switch e.status {
		case wce.SendStatusFailed:
			o.Failures[e.code]++
		case wce.SendStatusDelivered:
			o.Delivered++
		case wce.SendStatusRead:
			o.Delivered++
			o.Read++
		}
	}
	return o, nil
}

func (s *store) WaveRecipients(id string, number int) ([]sc.Recipient, error) {
	var out []sc.Recipient
	for _, e := range s.entries[id] {
		if e.wave == number && e.status == wce.SendStatusPending {
			out = append(out, sc.Recipient{EntryID: e.id, PhoneNumber: "5584999" + e.id[len(e.id)-5:]})
		}
	}
	return out, nil
}

func (s *store) process(id string, number int, failEvery int, code int) {
	i := 0
	for _, e := range s.entries[id] {
		if e.wave != number || e.status != wce.SendStatusPending {
			continue
		}
		i++
		if failEvery > 0 && i%failEvery == 0 {
			e.status, e.code = wce.SendStatusFailed, code
			continue
		}
		e.status = wce.SendStatusDelivered
	}
}

func (s *store) processSome(id string, number, count int) {
	for _, e := range s.entries[id] {
		if count == 0 {
			return
		}
		if e.wave == number && e.status == wce.SendStatusPending {
			e.status = wce.SendStatusDelivered
			count--
		}
	}
}

func (s *store) failOne(id string, number, code int) string {
	for _, e := range s.entries[id] {
		if e.wave == number && e.status == wce.SendStatusPending {
			e.status, e.code = wce.SendStatusFailed, code
			return e.id
		}
	}
	return ""
}

func (s *store) waveCount(id string) int {
	return len(s.waves[id])
}

type fakeHealth struct {
	templates  map[string]sc.TemplateState
	phones     map[string]sc.PhoneState
	freshFails bool
	freshReads int
}

func newHealth() *fakeHealth {
	return &fakeHealth{templates: map[string]sc.TemplateState{}, phones: map[string]sc.PhoneState{}}
}

func (h *fakeHealth) Template(_ context.Context, id string) (sc.TemplateState, error) {
	if t, ok := h.templates[id]; ok {
		return t, nil
	}
	return sc.TemplateState{Status: "APPROVED", Quality: "GREEN", Category: "UTILITY"}, nil
}

func (h *fakeHealth) Phone(_ context.Context, id string, fresh bool) (sc.PhoneState, error) {
	if fresh {
		h.freshReads++
		if h.freshFails {
			return sc.PhoneState{}, errBroken
		}
	}
	if p, ok := h.phones[id]; ok {
		return p, nil
	}
	return sc.PhoneState{Status: "CONNECTED", Quality: "GREEN"}, nil
}

type fakeConsumer struct {
	subscribed map[string]bool
	calls      []string
	pauseFails int
}

func newConsumer() *fakeConsumer { return &fakeConsumer{subscribed: map[string]bool{}} }

func (c *fakeConsumer) SubscribeToCampaign(id string) error {
	c.calls = append(c.calls, "subscribe:"+id)
	c.subscribed[id] = true
	return nil
}

func (c *fakeConsumer) PauseCampaignConsumer(id string) error {
	if c.pauseFails > 0 {
		c.pauseFails--
		return errBroken
	}
	c.calls = append(c.calls, "pause:"+id)
	c.subscribed[id] = false
	return nil
}

func (c *fakeConsumer) ResumeCampaignConsumer(id string) error {
	c.calls = append(c.calls, "resume:"+id)
	c.subscribed[id] = true
	return nil
}

func (c *fakeConsumer) StopCampaignConsumer(id string) error {
	c.calls = append(c.calls, "stop:"+id)
	c.subscribed[id] = false
	return nil
}

func (c *fakeConsumer) IsSubscribed(id string) bool { return c.subscribed[id] }

type fakeQueue struct {
	enqueued map[string][]string
	fails    int
}

func (q *fakeQueue) Enqueue(id string, messages []campaignqueue.Message) error {
	if q.fails > 0 {
		q.fails--
		return errBroken
	}
	if q.enqueued == nil {
		q.enqueued = map[string][]string{}
	}
	for _, m := range messages {
		q.enqueued[id] = append(q.enqueued[id], m.EntryID)
	}
	return nil
}

type fakeCaps struct{ usage *balance.SendCapUsage }

func (c *fakeCaps) MonthlySendCapUsage(string, time.Time) (*balance.SendCapUsage, error) {
	return c.usage, nil
}

type fakeNotifier struct{ held []sc.Decision }

func (n *fakeNotifier) Held(_ *wc.Campaign, _ int, d sc.Decision, _ time.Time) {
	n.held = append(n.held, d)
}

type memLocks struct{ keys map[string]bool }

func (m *memLocks) SetNX(key, _ string, _ time.Duration) (bool, error) {
	if m.keys == nil {
		m.keys = map[string]bool{}
	}
	if m.keys[key] {
		return false, nil
	}
	m.keys[key] = true
	return true, nil
}

func (m *memLocks) Del(keys ...string) error {
	for _, k := range keys {
		delete(m.keys, k)
	}
	return nil
}
