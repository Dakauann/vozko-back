package emailhygiene

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	h "vozko/domain/emailhygiene"
)

var ErrResolverUnhealthy = errors.New("emailhygiene: the canary domain did not resolve, our resolver is not trustworthy right now")

const runDueBudget = 50 * time.Second

type DomainDeps struct {
	Resolver h.MailDomainResolver
	Checks   h.DomainCheckStore
	Kick     h.DomainCheckKick
}

type Domains struct {
	d   DomainDeps
	now func() time.Time
	sem chan struct{}
}

func NewDomains(d DomainDeps) *Domains {
	return &Domains{d: d, now: func() time.Time { return time.Now().UTC() }, sem: make(chan struct{}, h.LookupConcurrency)}
}

func (s *Domains) Request(ctx context.Context, domains []string) error {
	queued, err := s.d.Checks.Request(ctx, distinct(domains), s.now())
	if err != nil {
		return err
	}
	if queued == 0 {
		return nil
	}
	if err := s.d.Kick.Kick(); err != nil {
		log.Printf("[emailhygiene] %d domains queued but the checker kick failed; the minute job picks them up: %v", queued, err)
	}
	return nil
}

func (s *Domains) Execute(ctx context.Context) error {
	return s.RunDue(ctx)
}

func (s *Domains) RunDue(ctx context.Context) error {
	started := s.now()
	for {
		claimed, err := s.d.Checks.ClaimDue(ctx, s.now(), h.CheckBatchSize, h.CheckLease)
		if err != nil {
			return err
		}
		if len(claimed) == 0 {
			return nil
		}
		prior := make(map[string]h.DomainCheck, len(claimed))
		domains := make([]string, 0, len(claimed))
		for _, c := range claimed {
			prior[c.Domain] = c
			domains = append(domains, c.Domain)
		}
		if _, err := s.resolveAndSave(ctx, domains, prior); err != nil {
			return err
		}
		if len(claimed) < h.CheckBatchSize || s.now().Sub(started) > runDueBudget {
			return nil
		}
	}
}

func (s *Domains) Resolve(ctx context.Context, domains []string) (map[string]h.DomainState, error) {
	domains = distinct(domains)
	out := make(map[string]h.DomainState, len(domains))
	stored, err := s.d.Checks.Read(ctx, domains)
	if err != nil {
		return nil, fmt.Errorf("emailhygiene: reading domain checks: %w", err)
	}
	now := s.now()
	var missing []string
	for _, domain := range domains {
		c, ok := stored[domain]
		if ok && !c.Stale(now) {
			out[domain] = c.State
			continue
		}
		missing = append(missing, domain)
	}
	if len(missing) == 0 {
		return out, nil
	}
	resolved, err := s.resolveAndSave(ctx, missing, stored)
	if errors.Is(err, ErrResolverUnhealthy) {
		for _, domain := range missing {
			out[domain] = h.DomainUnknown
		}
		return out, s.Request(ctx, missing)
	}
	if err != nil {
		return nil, err
	}
	for domain, verdict := range resolved {
		out[domain] = verdict.State
	}
	return out, nil
}

func (s *Domains) resolveAndSave(ctx context.Context, domains []string, prior map[string]h.DomainCheck) (map[string]h.DomainVerdict, error) {
	if !h.CanaryHealthy(h.Classify(s.d.Resolver.Resolve(ctx, h.CanaryDomain))) {
		return nil, ErrResolverUnhealthy
	}
	verdicts := s.lookup(ctx, domains)
	now := s.now()
	results := make([]h.DomainResult, 0, len(domains))
	for _, domain := range domains {
		v := verdicts[domain]
		attempts := 0
		if v.State == h.DomainUnknown {
			attempts = prior[domain].Attempts + 1
		}
		results = append(results, h.DomainResult{Domain: domain, Verdict: v, Attempts: attempts, Settled: h.Settle(v, attempts, now)})
	}
	if err := s.d.Checks.Save(ctx, results, now); err != nil {
		return nil, err
	}
	return verdicts, nil
}

func (s *Domains) lookup(ctx context.Context, domains []string) map[string]h.DomainVerdict {
	out := make(map[string]h.DomainVerdict, len(domains))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, domain := range domains {
		wg.Add(1)
		s.sem <- struct{}{}
		go func(domain string) {
			defer wg.Done()
			defer func() { <-s.sem }()
			v := h.Classify(s.d.Resolver.Resolve(ctx, domain))
			mu.Lock()
			out[domain] = v
			mu.Unlock()
		}(domain)
	}
	wg.Wait()
	return out
}

func distinct(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok || v == "" {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
