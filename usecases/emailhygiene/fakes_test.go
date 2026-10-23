package emailhygiene

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"vozko/domain/emailaddress"
	h "vozko/domain/emailhygiene"
)

var clock = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

type fakeResolver struct {
	mu      sync.Mutex
	answers map[string]h.DNSAnswer
	calls   map[string]int
	active  int
	peak    int
	delay   time.Duration
}

func newResolver() *fakeResolver {
	return &fakeResolver{answers: map[string]h.DNSAnswer{h.CanaryDomain: {MX: h.LookupFound, MXHosts: []string{"gmail-smtp-in.l.google.com"}}}, calls: map[string]int{}}
}

func (f *fakeResolver) set(domain string, a h.DNSAnswer) { f.answers[domain] = a }

func (f *fakeResolver) Resolve(_ context.Context, domain string) h.DNSAnswer {
	f.mu.Lock()
	f.calls[domain]++
	f.active++
	f.peak = max(f.peak, f.active)
	answer, ok := f.answers[domain]
	f.mu.Unlock()
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.mu.Lock()
	f.active--
	f.mu.Unlock()
	if !ok {
		return h.DNSAnswer{MX: h.LookupFound, MXHosts: []string{"mx." + domain}}
	}
	return answer
}

func (f *fakeResolver) count(domain string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[domain]
}

type fakeChecks struct {
	mu        sync.Mutex
	rows      map[string]h.DomainCheck
	due       map[string]time.Time
	saved     []h.DomainResult
	requested []string
	readErr   error
}

func newChecks() *fakeChecks {
	return &fakeChecks{rows: map[string]h.DomainCheck{}, due: map[string]time.Time{}}
}

func (f *fakeChecks) Read(_ context.Context, domains []string) (map[string]h.DomainCheck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return nil, f.readErr
	}
	out := map[string]h.DomainCheck{}
	for _, d := range domains {
		if c, ok := f.rows[d]; ok {
			out[d] = c
		}
	}
	return out, nil
}

func (f *fakeChecks) Request(_ context.Context, domains []string, now time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, d := range domains {
		f.requested = append(f.requested, d)
		c, ok := f.rows[d]
		_, queued := f.due[d]
		if !ok {
			f.rows[d] = h.DomainCheck{Domain: d, State: h.DomainPending, ExpiresAt: now}
			f.due[d] = now
			n++
			continue
		}
		if !queued && c.Stale(now) {
			f.due[d] = now
			n++
		}
	}
	return n, nil
}

func (f *fakeChecks) ClaimDue(_ context.Context, now time.Time, limit int, lease time.Duration) ([]h.DomainCheck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []h.DomainCheck
	for d, at := range f.due {
		if len(out) == limit {
			break
		}
		if !at.After(now) {
			out = append(out, f.rows[d])
			f.due[d] = now.Add(lease)
		}
	}
	return out, nil
}

func (f *fakeChecks) Save(_ context.Context, results []h.DomainResult, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range results {
		checked := now
		f.rows[r.Domain] = h.DomainCheck{Domain: r.Domain, State: r.Verdict.State, Detail: r.Verdict.Detail, CheckedAt: &checked, ExpiresAt: r.Settled.ExpiresAt, Attempts: r.Attempts}
		if r.Settled.DueAt != nil {
			f.due[r.Domain] = *r.Settled.DueAt
		} else {
			delete(f.due, r.Domain)
		}
		f.saved = append(f.saved, r)
	}
	return nil
}

func (f *fakeChecks) Prune(context.Context, time.Time, int) (int64, error) { return 0, nil }

type fakeKick struct{ kicks int }

func (k *fakeKick) Kick() error { k.kicks++; return nil }

type fakeLists struct {
	stored *h.StoredList
	saves  int
	kept   int
	err    error
}

func (f *fakeLists) Latest(context.Context) (*h.StoredList, error) { return f.stored, f.err }

func (f *fakeLists) Save(_ context.Context, content string, _ int, at time.Time) error {
	f.saves++
	f.stored = &h.StoredList{Content: content, FetchedAt: at}
	return nil
}

func (f *fakeLists) KeepNewest(_ context.Context, keep int) (int64, error) {
	f.kept = keep
	return 0, nil
}

type fakeSource struct {
	content string
	err     error
}

func (f fakeSource) Fetch(context.Context) (string, error) { return f.content, f.err }

func listWith(extra ...string) string {
	lines := make([]string, 0, h.MinDisposableDomains+len(extra))
	for i := 0; i < h.MinDisposableDomains; i++ {
		lines = append(lines, fmt.Sprintf("temp%05d.com", i))
	}
	return strings.Join(append(lines, extra...), "\n")
}

type fakeActivity struct {
	delivered []string
	engaged   []string
	streak    h.SoftStreak
	soft      []string
}

func (f *fakeActivity) Delivered(_ context.Context, ws string, hash h.AddressHash, msg string, campaign bool, _ time.Time) error {
	f.delivered = append(f.delivered, fmt.Sprintf("%s:%s:%v", ws, msg, campaign))
	return nil
}

func (f *fakeActivity) SoftBounced(_ context.Context, ws string, _ h.AddressHash, msg string, _ time.Time) (h.SoftStreak, error) {
	f.soft = append(f.soft, msg)
	return f.streak, nil
}

func (f *fakeActivity) Engaged(_ context.Context, ws string, _ h.AddressHash, at time.Time) error {
	f.engaged = append(f.engaged, ws+"@"+at.Format(time.RFC3339))
	return nil
}

func (f *fakeActivity) Prune(context.Context, time.Time, int) (int64, error) { return 0, nil }

type fakeBad struct{ added []h.AddressHash }

func (f *fakeBad) Add(_ context.Context, hash h.AddressHash, _ time.Time) error {
	f.added = append(f.added, hash)
	return nil
}

type fakeStanding struct {
	rows map[emailaddress.Address]h.Standing
}

func (f fakeStanding) Standing(_ context.Context, _ string, addresses []emailaddress.Address) (map[emailaddress.Address]h.Standing, error) {
	out := map[emailaddress.Address]h.Standing{}
	for _, a := range addresses {
		s, ok := f.rows[a]
		if !ok {
			s = h.Standing{Domain: h.DomainPending, DomainStale: true}
		}
		out[a] = s
	}
	return out, nil
}
