package emailsending

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	es "vozko/domain/emailsending"
)

const (
	SetupRunTimeout     = 4 * time.Minute
	SetupOutcomeTimeout = 10 * time.Second
)

type Setup struct {
	c *core
}

type DomainView struct {
	Domain    es.SendingDomain
	UsedToday int64
	CapToday  int64
}

type ProbationView struct {
	Days         int
	RequiredDays int
	Window       es.HealthWindow
	Outcome      es.PromotionOutcome
	Targets      es.PromotionTargets
}

type StatusView struct {
	Provisioned   bool
	Account       *es.SenderAccount
	Probation     *ProbationView
	Domains       []DomainView
	Senders       []SenderView
	RepliesPaused bool
	Setup         es.SetupProgress
	Subdomain     SubdomainView
	Activation    error
}

func (s *Setup) Status(ctx context.Context, workspaceID string) (StatusView, error) {
	c := s.c
	acct, err := c.account(ctx, workspaceID)
	if errors.Is(err, es.ErrAccountNotProvisioned) {
		suggested, err := c.suggestedSubdomain(ctx, workspaceID)
		if err != nil {
			return StatusView{}, err
		}
		return StatusView{Setup: es.SetupProgressOf(nil), Subdomain: suggested, Activation: c.setupAllowed(workspaceID)}, nil
	}
	if err != nil {
		return StatusView{}, err
	}
	now := c.Now()
	view := StatusView{Provisioned: true, Account: acct, Activation: c.setupAllowed(workspaceID)}
	if view.Subdomain, err = c.subdomainView(ctx, acct); err != nil {
		return StatusView{}, err
	}
	domains, err := c.Domains.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return StatusView{}, err
	}
	day := es.DayKey(now)
	for _, d := range domains {
		view.RepliesPaused = view.RepliesPaused || (d.ReceivesReplies() && d.RepliesPaused())
		if !d.Listed() {
			continue
		}
		used, err := c.Usage.Used(ctx, d.ID, day)
		if err != nil {
			return StatusView{}, err
		}
		view.Domains = append(view.Domains, DomainView{Domain: d, UsedToday: used, CapToday: es.DailyCap(d)})
	}
	view.Setup = es.SetupProgressOf(domains)
	senders, err := c.Senders.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return StatusView{}, err
	}
	if view.Senders, err = senderViews(senders, domains); err != nil {
		return StatusView{}, err
	}
	if acct.Mode == es.ModeProbation {
		window, err := c.Messages.HealthWindow(ctx, workspaceID, es.PromotionWindowStart(now))
		if err != nil {
			return StatusView{}, err
		}
		view.Probation = &ProbationView{
			Days:         probationDays(acct.ProbationStartedAt, now),
			RequiredDays: es.PromotionCleanDays,
			Window:       window,
			Outcome:      es.Promotion(*acct, window, now),
			Targets:      es.CurrentPromotionTargets(),
		}
	}
	return view, nil
}

func probationDays(startedAt, now time.Time) int {
	if startedAt.IsZero() || startedAt.After(now) {
		return 0
	}
	days := int(now.Sub(startedAt).Hours() / 24)
	if days > es.PromotionCleanDays {
		return es.PromotionCleanDays
	}
	return days
}

func (s *Setup) Provision(ctx context.Context, workspaceID, slug string) (StatusView, error) {
	c := s.c
	if err := c.setupAllowed(workspaceID); err != nil {
		return StatusView{}, err
	}
	acct, err := c.ensureAccount(ctx, workspaceID, slug)
	if err != nil {
		return StatusView{}, err
	}
	if slug != "" {
		if err := c.changeSlug(ctx, acct, slug); err != nil {
			return StatusView{}, err
		}
	}
	return s.startWorkspaceSetup(ctx, acct)
}

func (s *Setup) Complete(ctx context.Context, workspaceID string) error {
	return s.c.completeSetup(es.WithProviderLane(ctx, es.LaneAccount), workspaceID, true)
}

func (c *core) sharedKindOf(acct *es.SenderAccount) es.DomainKind {
	if acct.Mode == es.ModeProbation {
		return es.DomainSharedProbation
	}
	return es.DomainSharedTrusted
}

func (c *core) ensureAccount(ctx context.Context, workspaceID, chosen string) (*es.SenderAccount, error) {
	acct, err := c.Accounts.GetByWorkspace(ctx, workspaceID)
	if err == nil {
		return acct, nil
	}
	if !errors.Is(err, es.ErrAccountNotFound) {
		return nil, err
	}
	tenantName, err := es.TenantNameFor(workspaceID)
	if err != nil {
		return nil, err
	}
	slug := chosen
	if slug == "" {
		slug, err = c.suggestSlug(ctx, workspaceID)
	} else {
		err = c.chosenSlug(ctx, workspaceID, "", slug)
	}
	if err != nil {
		return nil, err
	}
	acct = &es.SenderAccount{
		WorkspaceID:        workspaceID,
		Provider:           c.Provider.Name(),
		TenantName:         tenantName,
		Mode:               es.ModeProbation,
		Status:             es.AccountActive,
		Slug:               slug,
		ProbationStartedAt: c.Now(),
	}
	if err := c.Accounts.Create(ctx, acct); err != nil {
		existing, getErr := c.Accounts.GetByWorkspace(ctx, workspaceID)
		if getErr == nil {
			return existing, nil
		}
		return nil, err
	}
	return acct, nil
}

func (c *core) sharedDomainRow(ctx context.Context, acct *es.SenderAccount, kind es.DomainKind) (*es.SendingDomain, error) {
	fqdn, err := es.SharedSubdomain(acct.Slug, c.sharedDomainFor(kind))
	if err != nil {
		return nil, err
	}
	layout, err := es.LayoutFor(kind, fqdn)
	if err != nil {
		return nil, err
	}
	d, err := c.Domains.GetByDomain(ctx, fqdn)
	if errors.Is(err, es.ErrDomainNotFound) {
		d = &es.SendingDomain{
			WorkspaceID:    acct.WorkspaceID,
			AccountID:      acct.ID,
			Domain:         fqdn,
			Kind:           kind,
			Status:         es.DomainPending,
			ConnectMethod:  es.ConnectAutomatic,
			MailFromDomain: layout.MailFromDomain,
			ReplyDomain:    layout.ReplyDomain,
		}
		if err = c.Domains.Create(ctx, d); errors.Is(err, es.ErrDomainTaken) {
			d, err = c.Domains.GetByDomain(ctx, fqdn)
		}
	}
	if err != nil {
		return nil, err
	}
	if d.WorkspaceID != acct.WorkspaceID {
		return nil, es.ErrDomainTaken
	}
	return d, nil
}

func (c *core) startSetup(ctx context.Context, acct *es.SenderAccount, domains ...*es.SendingDomain) error {
	awaiting := make([]*es.SendingDomain, 0, len(domains))
	for _, d := range domains {
		if !d.AwaitsSetup() {
			continue
		}
		awaiting = append(awaiting, d)
		if d.RestartSetup() {
			if err := c.Domains.Update(ctx, d); err != nil {
				return err
			}
		}
	}
	if len(awaiting) == 0 && acct.Status != es.AccountDormant {
		return nil
	}
	if err := c.SetupWake.ScheduleFire(acct.WorkspaceID, c.Now()); err != nil {
		log.Printf("[emailsending] the setup run of workspace %s was not queued: %v", acct.WorkspaceID, err)
		_ = c.failSetup(ctx, "queue", err, awaiting...)
	}
	return nil
}

func (c *core) completeSetup(ctx context.Context, workspaceID string, wakeDormant bool) error {
	ctx, cancel := context.WithTimeout(ctx, SetupRunTimeout)
	defer cancel()
	return c.withTenantLock(ctx, workspaceID, func() error {
		acct, err := c.account(ctx, workspaceID)
		if err != nil {
			return err
		}
		domains, err := c.Domains.ListByWorkspace(ctx, workspaceID)
		if err != nil {
			return err
		}
		var errs []error
		for _, d := range domainsWhere(domains, es.SendingDomain.AwaitsRelease) {
			errs = append(errs, c.releaseDomain(es.WithProviderLane(ctx, es.LaneBackground), acct, d))
		}
		return errors.Join(append(errs, c.setUpDomains(ctx, acct, domainsWhere(domains, es.SendingDomain.AwaitsSetup), wakeDormant))...)
	})
}

func (c *core) setUpDomains(ctx context.Context, acct *es.SenderAccount, pending []*es.SendingDomain, wakeDormant bool) error {
	if acct.Status == es.AccountDormant {
		if !wakeDormant {
			return nil
		}
		if err := c.wake(ctx, acct); err != nil {
			c.alertOps(ctx, "email tenant reactivation failed", fmt.Sprintf("workspace %s: %v", acct.WorkspaceID, err))
			return c.failSetup(ctx, "revive tenant", err, pending...)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	if err := c.ensureTenant(ctx, acct); err != nil {
		return c.failSetup(ctx, "tenant", err, pending...)
	}
	var errs []error
	for _, d := range pending {
		errs = append(errs, c.setupDomain(ctx, acct, d))
	}
	return errors.Join(errs...)
}

func (c *core) domainsAwaitingSetup(ctx context.Context, workspaceID string) ([]*es.SendingDomain, error) {
	domains, err := c.Domains.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return domainsWhere(domains, es.SendingDomain.AwaitsSetup), nil
}

func domainsWhere(domains []es.SendingDomain, keep func(es.SendingDomain) bool) []*es.SendingDomain {
	var out []*es.SendingDomain
	for i := range domains {
		if keep(domains[i]) {
			out = append(out, &domains[i])
		}
	}
	return out
}

func (c *core) ensureTenant(ctx context.Context, acct *es.SenderAccount) error {
	if acct.TenantRef != "" {
		return nil
	}
	tenant, err := c.Provider.EnsureTenant(ctx, es.TenantSpec{Name: acct.TenantName})
	if err != nil || tenant.Ref == "" {
		return err
	}
	acct.TenantRef = tenant.Ref
	return c.Accounts.Update(ctx, acct)
}

func (c *core) setupDomain(ctx context.Context, acct *es.SenderAccount, d *es.SendingDomain) error {
	layout, err := es.LayoutFor(d.Kind, d.Domain)
	if err != nil {
		return err
	}
	if d.Kind.Shared() {
		shared := c.sharedDomainFor(d.Kind)
		return c.setupIdentity(ctx, acct, d, layout, es.DMARCState{}, func(records []es.DNSRecord) error {
			return c.DNS.UpsertRecords(ctx, shared, records)
		})
	}
	existing, err := c.DMARC.LookupDMARC(ctx, d.Domain)
	if err != nil {
		return c.failSetup(ctx, "dmarc lookup", err, d)
	}
	return c.setupIdentity(ctx, acct, d, layout, es.DMARCState{Existing: existing}, nil)
}

func (c *core) setupIdentity(ctx context.Context, acct *es.SenderAccount, d *es.SendingDomain, layout es.DomainLayout, dmarc es.DMARCState, publish func([]es.DNSRecord) error) error {
	setup, err := c.Provider.EnsureIdentity(ctx, es.IdentitySpec{TenantName: acct.TenantName, Domain: d.Domain, MailFromDomain: layout.MailFromDomain})
	if err != nil {
		return c.failSetup(ctx, "identity", err, d)
	}
	records := es.RequiredRecords(layout, setup, dmarc)
	if publish != nil {
		if err := publish(records); err != nil {
			return c.failSetup(ctx, "dns", err, d)
		}
	}
	if err := c.Routes.EnsureReplyRoute(ctx, layout.ReplyDomain); err != nil {
		if errors.Is(err, es.ErrInboundCapacityReached) {
			c.opsAlert(ctx, "Regras de recebimento de e-mail cheias", fmt.Sprintf("o domínio de resposta %s não coube nas receipt rules do provedor: %v", layout.ReplyDomain, err))
		}
		return c.failSetup(ctx, "inbound", err, d)
	}
	d.CompleteSetup(records, layout, c.Now())
	record, cancel := outcomeContext(ctx)
	defer cancel()
	return c.Domains.Update(record, d)
}

func (c *core) failSetup(ctx context.Context, step string, cause error, domains ...*es.SendingDomain) error {
	failure := es.SetupFailureFor(cause)
	reason := step + ": " + truncate(cause.Error(), 300)
	record, cancel := outcomeContext(ctx)
	defer cancel()
	for _, d := range domains {
		d.FailSetup(failure, reason)
		if err := c.Domains.Update(record, d); err != nil {
			log.Printf("[emailsending] could not record the %s failure of %s: %v", step, d.Domain, err)
		}
	}
	if failure == es.SetupFailureReplyCapacity {
		return fmt.Errorf("%w: %s: %v", ErrReplyCapacityReached, step, cause)
	}
	return providerFailure(step, cause)
}

func outcomeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), SetupOutcomeTimeout)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}
