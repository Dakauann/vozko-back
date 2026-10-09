package campaignguard

import (
	"context"
	"errors"
	"sync"
	"time"

	"vozko/domain/campaign"
)

var errBoom = errors.New("boom")

type fixedDays struct {
	mu    sync.Mutex
	days  int
	err   error
	calls int
}

func (p *fixedDays) SpamProtectionDays(context.Context, string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.days, p.err
}

type sendLog struct {
	mu        sync.Mutex
	last      map[string]time.Time
	err       error
	batches   [][]string
	singles   int
	recorded  []string
	recordErr error
}

func (s *sendLog) Record(leadID, businessPhoneID, campaignID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recorded = append(s.recorded, leadID+"|"+businessPhoneID+"|"+campaignID)
	return s.recordErr
}

func (s *sendLog) GetLastSendTime(leadID, _ string) (*time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.singles++
	if s.err != nil {
		return nil, s.err
	}
	at, ok := s.last[leadID]
	if !ok {
		return nil, nil
	}
	return &at, nil
}

func (s *sendLog) GetLastSendTimesBatch(leadIDs []string, _ string) (map[string]time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batches = append(s.batches, append([]string(nil), leadIDs...))
	if s.err != nil {
		return nil, s.err
	}
	out := map[string]time.Time{}
	for _, id := range leadIDs {
		if at, ok := s.last[id]; ok {
			out[id] = at
		}
	}
	return out, nil
}

type leadBook struct {
	facts   map[string]campaign.LeadFacts
	err     error
	batches [][]string
}

func (b *leadBook) LeadFacts(_ context.Context, _ string, leadIDs []string) (map[string]campaign.LeadFacts, error) {
	b.batches = append(b.batches, append([]string(nil), leadIDs...))
	if b.err != nil {
		return nil, b.err
	}
	out := map[string]campaign.LeadFacts{}
	for _, id := range leadIDs {
		if f, ok := b.facts[id]; ok {
			out[id] = f
		}
	}
	return out, nil
}

func reachable() campaign.LeadFacts {
	return campaign.LeadFacts{Found: true, HasIdentity: true, HasConsent: true}
}

type claimBook struct {
	mu      sync.Mutex
	held    map[string]time.Duration
	setErr  error
	delErr  error
	deleted []string
}

func newClaimBook() *claimBook {
	return &claimBook{held: map[string]time.Duration{}}
}

func (c *claimBook) SetNX(key, _ string, ttl time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.setErr != nil {
		return false, c.setErr
	}
	if _, taken := c.held[key]; taken {
		return false, nil
	}
	c.held[key] = ttl
	return true, nil
}

func (c *claimBook) Del(keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range keys {
		delete(c.held, key)
		c.deleted = append(c.deleted, key)
	}
	return c.delErr
}

func (c *claimBook) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.held)
}
