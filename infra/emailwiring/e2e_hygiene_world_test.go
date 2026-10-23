package emailwiring_test

import (
	"context"
	"sync"

	"vozko/domain/emailaddress"
	h "vozko/domain/emailhygiene"
	es "vozko/domain/emailsending"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/emailhygiene/disposable"
	hyuc "vozko/usecases/emailhygiene"
)

type e2eResolver struct {
	mu      sync.Mutex
	answers map[string]h.DNSAnswer
	lookups map[string]int
}

func newE2EResolver() *e2eResolver {
	return &e2eResolver{answers: map[string]h.DNSAnswer{}, lookups: map[string]int{}}
}

func (r *e2eResolver) set(domain string, a h.DNSAnswer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.answers[domain] = a
}

func (r *e2eResolver) Resolve(_ context.Context, domain string) h.DNSAnswer {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookups[domain]++
	if a, ok := r.answers[domain]; ok {
		return a
	}
	return h.DNSAnswer{MX: h.LookupFound, MXHosts: []string{"mx." + domain}}
}

type snapshotSource struct{}

func (snapshotSource) Fetch(context.Context) (string, error) { return disposable.Snapshot(), nil }

type checkedBeforeReview struct {
	*hyuc.Service
}

func (c checkedBeforeReview) Standing(ctx context.Context, workspaceID string, addresses []emailaddress.Address) (map[emailaddress.Address]h.Standing, error) {
	standing, err := c.Service.Standing(ctx, workspaceID, addresses)
	if err != nil {
		return nil, err
	}
	var unchecked []string
	for address, s := range standing {
		if s.Domain == h.DomainPending {
			unchecked = append(unchecked, address.Domain())
		}
	}
	if len(unchecked) == 0 {
		return standing, nil
	}
	if _, err := c.Service.ResolveDomains(ctx, unchecked); err != nil {
		return nil, err
	}
	return c.Service.Standing(ctx, workspaceID, addresses)
}

type checkedBeforeSend struct {
	*hyuc.Service
}

func (c checkedBeforeSend) SendRefusal(ctx context.Context, workspaceID string, kind es.RunKind, to emailaddress.Address) error {
	if h.FollowsCampaignRule(kind) {
		standing, err := c.Service.Standing(ctx, workspaceID, []emailaddress.Address{to})
		if err != nil {
			return err
		}
		if standing[to].NeedsDomainCheck() {
			if _, err := c.Service.ResolveDomains(ctx, []string{to.Domain()}); err != nil {
				return err
			}
		}
	}
	return c.Service.SendRefusal(ctx, workspaceID, kind, to)
}

func (w *world) mailedBefore(ws *tenant, leads []schema.Lead) {
	w.t.Helper()
	ids := make([]string, len(leads))
	for i, l := range leads {
		ids[i] = l.ID
	}
	w.must(w.db.Exec(`INSERT INTO email_address_activity (workspace_id, address_hash, first_delivered_at, last_delivered_at, last_engaged_at, sunset_since,
  campaigns_since_engaged, soft_streak, last_outcome_message_id, touched_at)
SELECT workspace_id, sha256(convert_to(lower(email), 'UTF8')), now() - interval '30 days', now() - interval '30 days', now() - interval '30 days', now() - interval '30 days', 0, 0, '', now()
FROM leads WHERE workspace_id = ? AND id = ANY(?::uuid[]) AND email IS NOT NULL
ON CONFLICT DO NOTHING`, ws.ID, database.Texts(ids)).Error)
}
