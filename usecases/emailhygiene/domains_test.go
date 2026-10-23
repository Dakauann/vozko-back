package emailhygiene

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	h "vozko/domain/emailhygiene"
)

func newDomains(r *fakeResolver, checks *fakeChecks, kick *fakeKick) *Domains {
	d := NewDomains(DomainDeps{Resolver: r, Checks: checks, Kick: kick})
	d.now = func() time.Time { return clock }
	return d
}

func TestRequestQueuesMissingDomainsAndKicksTheCheckerOnce(t *testing.T) {
	checks, kick := newChecks(), &fakeKick{}
	d := newDomains(newResolver(), checks, kick)
	if err := d.Request(context.Background(), []string{"loja.com.br", "gmail.com", "loja.com.br"}); err != nil {
		t.Fatal(err)
	}
	if kick.kicks != 1 {
		t.Fatalf("kicks = %d, want one kick for the new domains", kick.kicks)
	}
	if err := d.Request(context.Background(), []string{"loja.com.br"}); err != nil {
		t.Fatal(err)
	}
	if kick.kicks != 1 {
		t.Fatal("a domain already queued never kicks again")
	}
}

func TestRunDueResolvesQueuedDomainsAndStoresTheirVerdicts(t *testing.T) {
	r, checks := newResolver(), newChecks()
	r.set("nomail.com.br", h.DNSAnswer{MX: h.LookupFound, MXHosts: []string{"."}})
	r.set("slow.com.br", h.DNSAnswer{MX: h.LookupTimedOut})
	d := newDomains(r, checks, &fakeKick{})
	if _, err := checks.Request(context.Background(), []string{"loja.com.br", "nomail.com.br", "slow.com.br"}, clock); err != nil {
		t.Fatal(err)
	}
	if err := d.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := map[string]h.DomainState{"loja.com.br": h.DomainAccepts, "nomail.com.br": h.DomainNoMail, "slow.com.br": h.DomainUnknown}
	for domain, state := range want {
		if got := checks.rows[domain].State; got != state {
			t.Fatalf("%s = %s, want %s", domain, got, state)
		}
	}
	if checks.rows["slow.com.br"].Attempts != 1 {
		t.Fatalf("an unknown verdict counts its attempts, got %d", checks.rows["slow.com.br"].Attempts)
	}
	if due, ok := checks.due["slow.com.br"]; !ok || !due.Equal(clock.Add(30*time.Minute)) {
		t.Fatalf("an unknown domain comes back after thirty minutes, due %v", due)
	}
	if _, ok := checks.due["loja.com.br"]; ok {
		t.Fatal("a settled domain leaves the queue")
	}
}

func TestRunDueStoresNothingWhenTheCanaryFails(t *testing.T) {
	r, checks := newResolver(), newChecks()
	r.set(h.CanaryDomain, h.DNSAnswer{MX: h.LookupTimedOut})
	d := newDomains(r, checks, &fakeKick{})
	if _, err := checks.Request(context.Background(), []string{"loja.com.br"}, clock); err != nil {
		t.Fatal(err)
	}
	err := d.RunDue(context.Background())
	if !errors.Is(err, ErrResolverUnhealthy) {
		t.Fatalf("err = %v, want ErrResolverUnhealthy", err)
	}
	if len(checks.saved) != 0 || checks.rows["loja.com.br"].State != h.DomainPending {
		t.Fatalf("our resolver failing must never mark a domain, saved %+v", checks.saved)
	}
	if r.count("loja.com.br") != 0 {
		t.Fatal("no domain is looked up behind a failed canary")
	}
}

func TestResolveUsesTheCacheAndLooksUpOnlyMissingOrStaleDomains(t *testing.T) {
	r, checks := newResolver(), newChecks()
	checks.rows["fresh.com.br"] = h.DomainCheck{Domain: "fresh.com.br", State: h.DomainNoMail, ExpiresAt: clock.Add(time.Hour)}
	checks.rows["stale.com.br"] = h.DomainCheck{Domain: "stale.com.br", State: h.DomainNoMail, ExpiresAt: clock.Add(-time.Hour)}
	d := newDomains(r, checks, &fakeKick{})
	got, err := d.Resolve(context.Background(), []string{"fresh.com.br", "stale.com.br", "new.com.br", "new.com.br"})
	if err != nil {
		t.Fatal(err)
	}
	if got["fresh.com.br"] != h.DomainNoMail || got["stale.com.br"] != h.DomainAccepts || got["new.com.br"] != h.DomainAccepts {
		t.Fatalf("got %+v", got)
	}
	if r.count("fresh.com.br") != 0 || r.count("stale.com.br") != 1 || r.count("new.com.br") != 1 {
		t.Fatalf("lookups fresh=%d stale=%d new=%d, a 1M list resolves each distinct domain once", r.count("fresh.com.br"), r.count("stale.com.br"), r.count("new.com.br"))
	}
}

func TestResolveAnswersUnknownWithoutStoringWhenTheResolverIsDown(t *testing.T) {
	r, checks := newResolver(), newChecks()
	r.set(h.CanaryDomain, h.DNSAnswer{MX: h.LookupFailed})
	kick := &fakeKick{}
	d := newDomains(r, checks, kick)
	got, err := d.Resolve(context.Background(), []string{"new.com.br"})
	if err != nil {
		t.Fatal(err)
	}
	if got["new.com.br"] != h.DomainUnknown {
		t.Fatalf("got %+v, an unchecked domain is unknown for this call, never valid", got)
	}
	if len(checks.saved) != 0 {
		t.Fatal("nothing is stored behind a failed canary")
	}
	if kick.kicks != 1 || checks.rows["new.com.br"].State != h.DomainPending {
		t.Fatal("the domain is queued so the checker resolves it later")
	}
}

func TestResolveBoundsConcurrentLookups(t *testing.T) {
	r, checks := newResolver(), newChecks()
	r.delay = 5 * time.Millisecond
	d := newDomains(r, checks, &fakeKick{})
	domains := make([]string, 100)
	for i := range domains {
		domains[i] = fmt.Sprintf("d%03d.com.br", i)
	}
	if _, err := d.Resolve(context.Background(), domains); err != nil {
		t.Fatal(err)
	}
	if r.peak > h.LookupConcurrency {
		t.Fatalf("peak concurrent lookups = %d, limit %d", r.peak, h.LookupConcurrency)
	}
	if len(checks.saved) != len(domains) {
		t.Fatalf("saved %d verdicts", len(checks.saved))
	}
}

func TestResolveFailsClosedWhenTheCacheCannotBeRead(t *testing.T) {
	checks := newChecks()
	checks.readErr = errors.New("db down")
	d := newDomains(newResolver(), checks, &fakeKick{})
	if _, err := d.Resolve(context.Background(), []string{"loja.com.br"}); err == nil {
		t.Fatal("an unreadable cache is an error, never a guess")
	}
}
