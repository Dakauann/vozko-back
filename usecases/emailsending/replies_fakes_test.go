package emailsending

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"sync"
	"time"

	es "vozko/domain/emailsending"
)

type blockCall struct {
	ip    netip.Addr
	until time.Time
}

type fakeRoutes struct {
	mu          sync.Mutex
	ensureErr   error
	ensured     []string
	removed     []string
	removeErr   error
	paused      []string
	resumed     []string
	failPause   map[string]bool
	failResume  map[string]bool
	adopted     [][]es.ReplyRoute
	adoptErr    error
	blocks      []blockCall
	blockNew    bool
	blockErr    error
	unblockedAt []time.Time
	unblockErr  error
	ipFilters   int
	filtersErr  error
	lanes       map[string][]es.ProviderLane
}

func (f *fakeRoutes) lane(ctx context.Context, call string) {
	if f.lanes == nil {
		f.lanes = map[string][]es.ProviderLane{}
	}
	f.lanes[call] = append(f.lanes[call], es.ProviderLaneOf(ctx))
}

func (f *fakeRoutes) lanesOf(call string) []es.ProviderLane {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]es.ProviderLane(nil), f.lanes[call]...)
}

func (f *fakeRoutes) IPFilterCount(ctx context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lane(ctx, "filters")
	return f.ipFilters, f.filtersErr
}

func (f *fakeRoutes) EnsureReplyRoute(_ context.Context, domain string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ensureErr != nil {
		return f.ensureErr
	}
	f.ensured = append(f.ensured, domain)
	return nil
}

func (f *fakeRoutes) PauseReplies(ctx context.Context, domain string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lane(ctx, "pause")
	if f.failPause[domain] {
		return errors.New("ses throttled")
	}
	f.paused = append(f.paused, domain)
	return nil
}

func (f *fakeRoutes) ResumeReplies(ctx context.Context, domain string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lane(ctx, "resume")
	if f.failResume[domain] {
		return errors.New("ses throttled")
	}
	f.resumed = append(f.resumed, domain)
	return nil
}

func (f *fakeRoutes) RemoveReplyRoute(ctx context.Context, domain string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lane(ctx, "remove route")
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = append(f.removed, domain)
	return nil
}

func (f *fakeRoutes) AdoptLegacyRoutes(ctx context.Context, routes []es.ReplyRoute) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lane(ctx, "adopt")
	if f.adoptErr != nil {
		return f.adoptErr
	}
	f.adopted = append(f.adopted, routes)
	return nil
}

func (f *fakeRoutes) BlockSender(ctx context.Context, ip netip.Addr, until time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lane(ctx, "block")
	if f.blockErr != nil {
		return false, f.blockErr
	}
	f.blocks = append(f.blocks, blockCall{ip: ip, until: until})
	return f.blockNew, nil
}

func (f *fakeRoutes) UnblockExpired(ctx context.Context, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lane(ctx, "unblock")
	f.unblockedAt = append(f.unblockedAt, now)
	return f.unblockErr
}

type memReplyPauses struct {
	domains  *memDomains
	balances map[string]int64
	markErr  error
	listErr  error
}

func (m *memReplyPauses) MarkRepliesPaused(_ context.Context, ws, id string, at time.Time) error {
	if m.markErr != nil {
		return m.markErr
	}
	return m.set(ws, id, &at)
}

func (m *memReplyPauses) ClearRepliesPaused(_ context.Context, ws, id string) error {
	return m.set(ws, id, nil)
}

func (m *memReplyPauses) set(ws, id string, at *time.Time) error {
	m.domains.mu.Lock()
	defer m.domains.mu.Unlock()
	d, ok := m.domains.rows[id]
	if !ok || d.WorkspaceID != ws {
		return es.ErrDomainNotFound
	}
	d.RepliesPausedAt = at
	m.domains.rows[id] = d
	return nil
}

func (m *memReplyPauses) ListReplyHoldMismatches(context.Context) ([]es.ReplyHold, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	seen := map[string]bool{}
	var out []es.ReplyHold
	for _, d := range m.domains.list(func(es.SendingDomain) bool { return true }) {
		balance, ok := m.balances[d.WorkspaceID]
		if !ok || seen[d.WorkspaceID] {
			continue
		}
		if d.RepliesPaused() == (balance < 0) || !d.ReceivesReplies() {
			continue
		}
		seen[d.WorkspaceID] = true
		out = append(out, es.ReplyHold{WorkspaceID: d.WorkspaceID, BalanceMicros: balance})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WorkspaceID < out[j].WorkspaceID })
	return out, nil
}

func (m *memReplyPauses) ListAll(context.Context) ([]es.SendingDomain, error) {
	return m.domains.list(func(es.SendingDomain) bool { return true }), nil
}

type fakeTraffic struct {
	mu     sync.Mutex
	counts map[string]int64
	err    error
}

func (f *fakeTraffic) IncrWithTTL(key string, _ time.Duration) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	f.counts[key]++
	return f.counts[key], nil
}
