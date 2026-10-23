package emailsending

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
)

const (
	VerificationTimeout   = 72 * time.Hour
	verificationBatchSize = 100
)

type Domains struct {
	c *core
}

func (d *Domains) Get(ctx context.Context, workspaceID, id string) (*es.SendingDomain, error) {
	return d.c.Domains.Get(ctx, workspaceID, id)
}

func (d *Domains) Connect(ctx context.Context, workspaceID, rawDomain string) (*es.SendingDomain, error) {
	c := d.c
	domain, err := emailaddress.NormalizeDomain(rawDomain)
	if err != nil {
		return nil, ErrDomainInvalid
	}
	if emailaddress.IsFreeMailbox(domain) {
		return nil, ErrDomainFreeMailbox
	}
	if c.isSharedDomain(domain) {
		return nil, ErrDomainShared
	}
	if err := c.setupAllowed(workspaceID); err != nil {
		return nil, err
	}
	acct, err := c.account(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	layout, err := es.LayoutFor(es.DomainOwn, domain)
	if err != nil {
		return nil, err
	}
	if other, err := c.Domains.GetByReplyDomain(ctx, layout.ReplyDomain); err == nil && other.WorkspaceID != workspaceID {
		return nil, es.ErrDomainTaken
	} else if err != nil && !errors.Is(err, es.ErrDomainNotFound) {
		return nil, err
	}
	sd, err := c.Domains.GetByDomain(ctx, domain)
	switch {
	case errors.Is(err, es.ErrDomainNotFound):
		sd = &es.SendingDomain{
			WorkspaceID:    workspaceID,
			AccountID:      acct.ID,
			Domain:         domain,
			Kind:           es.DomainOwn,
			Status:         es.DomainPending,
			ConnectMethod:  es.ConnectManual,
			MailFromDomain: layout.MailFromDomain,
			ReplyDomain:    layout.ReplyDomain,
		}
		if err := c.Domains.Create(ctx, sd); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	case sd.WorkspaceID != workspaceID || sd.Kind != es.DomainOwn:
		return nil, es.ErrDomainTaken
	default:
		restart := sd.Retired || sd.Status == es.DomainFailed
		if sd.Retired {
			sd.Reinstate()
		}
		if restart {
			sd.RestartSetup()
			if err := c.Domains.Update(ctx, sd); err != nil {
				return nil, err
			}
		}
	}
	if err := c.startSetup(ctx, acct, sd); err != nil {
		return nil, err
	}
	return sd, nil
}

func (d *Domains) Recheck(ctx context.Context, workspaceID, id string) (*es.SendingDomain, error) {
	sd, err := d.c.Domains.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if !sd.Actions().Recheck {
		return sd, nil
	}
	if sd.AwaitsSetup() {
		acct, err := d.c.account(ctx, workspaceID)
		if err != nil {
			return nil, err
		}
		if err := d.c.startSetup(ctx, acct, sd); err != nil {
			return nil, err
		}
		return sd, nil
	}
	if err := d.c.recheck(ctx, sd); err != nil {
		return nil, err
	}
	return sd, nil
}

func (d *Domains) Remove(ctx context.Context, workspaceID, id string) error {
	c := d.c
	sd, err := c.Domains.Get(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if sd.Kind.Shared() {
		return ErrSharedDomainRemoval
	}
	if sd.Retired {
		return nil
	}
	senders, err := c.Senders.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	for _, s := range senders {
		if s.DomainID == sd.ID {
			return ErrDomainInUse
		}
	}
	acct, err := c.account(ctx, workspaceID)
	if err != nil {
		return err
	}
	if err := c.dropProviderSetup(ctx, acct, sd); err != nil {
		return err
	}
	sd.Retired = true
	if err := c.Domains.Update(ctx, sd); err != nil {
		return err
	}
	c.resolve(ctx, workspaceID, domainFailedKey(sd.ID), spamRateKey(sd.ID))
	return c.settleModeAfterRemoval(ctx, acct)
}

func (c *core) settleModeAfterRemoval(ctx context.Context, acct *es.SenderAccount) error {
	if acct.Mode != es.ModeOwnDomain {
		return nil
	}
	domains, err := c.Domains.ListByWorkspace(ctx, acct.WorkspaceID)
	if err != nil {
		return err
	}
	for _, d := range domains {
		if d.Kind == es.DomainOwn && d.Sendable() {
			return nil
		}
	}
	acct.Mode = es.ModeProbation
	if acct.PromotedAt != nil {
		acct.Mode = es.ModeTrusted
	}
	return c.Accounts.Update(ctx, acct)
}

func (c *core) recheck(ctx context.Context, sd *es.SendingDomain) error {
	state, err := c.Provider.IdentityStatus(ctx, sd.Domain)
	if err != nil {
		return providerFailure("identity status", err)
	}
	now := c.Now()
	previous := sd.Status
	next := es.NextDomainStatus(previous, state)
	if previous == es.DomainFailed && next != es.DomainVerified {
		next = es.DomainFailed
	}
	sd.LastCheckedAt = &now
	switch {
	case next == es.DomainFailed && previous != es.DomainFailed:
		sd.FailureReason = state.FailureReason
	case next == es.DomainVerifying && sd.VerifyingSince == nil:
		sd.VerifyingSince = &now
	}
	if next == es.DomainVerifying && sd.VerifyingSince != nil && now.Sub(*sd.VerifyingSince) > VerificationTimeout && previous != es.DomainVerified {
		next = es.DomainFailed
		sd.FailureReason = "verification_timeout"
	}
	if next == es.DomainVerifying && previous == es.DomainVerified {
		sd.VerifyingSince = &now
	}
	sd.Status = next
	sd.Records = es.CheckRecords(sd.Records, state, c.lookupRecords(ctx, sd.Records))
	if next == es.DomainVerified && previous != es.DomainVerified {
		sd.VerifiedAt = &now
		sd.FailureReason = ""
	}
	if err := c.Domains.Update(ctx, sd); err != nil {
		return err
	}
	switch {
	case next == es.DomainVerified && previous != es.DomainVerified:
		return c.onVerified(ctx, sd)
	case next == es.DomainFailed && previous != es.DomainFailed:
		c.alertDomainFailed(ctx, sd)
	case next == es.DomainVerifying && previous == es.DomainVerified:
		c.alertDomainFailed(ctx, sd)
	}
	return nil
}

func (c *core) lookupRecords(ctx context.Context, records []es.DNSRecord) es.DNSAnswers {
	mx, txt := es.RecordLookups(records)
	return es.DNSAnswers{MX: c.lookupAll(ctx, mx, c.Records.LookupMX), TXT: c.lookupAll(ctx, txt, c.Records.LookupTXT)}
}

func (c *core) lookupAll(ctx context.Context, names []string, lookup func(context.Context, string) ([]string, error)) map[string]es.DNSAnswer {
	out := make(map[string]es.DNSAnswer, len(names))
	for _, name := range names {
		values, err := lookup(ctx, name)
		if err != nil {
			log.Printf("[emailsending] dns lookup of %s failed: %v", name, err)
			out[name] = es.DNSAnswer{Failed: true}
			continue
		}
		out[name] = es.DNSAnswer{Values: values}
	}
	return out
}

func (c *core) onVerified(ctx context.Context, sd *es.SendingDomain) error {
	acct, err := c.account(ctx, sd.WorkspaceID)
	if err != nil {
		return err
	}
	switch sd.Kind {
	case es.DomainOwn:
		if acct.Mode != es.ModeOwnDomain {
			acct.Mode = es.ModeOwnDomain
			if err := c.Accounts.Update(ctx, acct); err != nil {
				return err
			}
		}
	case es.DomainSharedTrusted:
		if err := c.completePromotion(ctx, acct, sd); err != nil {
			return err
		}
	}
	if sd.Kind.Shared() {
		if err := c.ensureDefaultSender(ctx, acct, sd); err != nil {
			return err
		}
	}
	c.notifyDomainVerified(ctx, sd)
	return nil
}

func (c *core) ensureDefaultSender(ctx context.Context, acct *es.SenderAccount, sd *es.SendingDomain) error {
	if _, err := c.Senders.GetDefault(ctx, acct.WorkspaceID); err == nil {
		return nil
	} else if !errors.Is(err, es.ErrSenderNotFound) {
		return err
	}
	name, err := c.Workspaces.WorkspaceName(ctx, acct.WorkspaceID)
	if err != nil {
		return fmt.Errorf("emailsending: workspace name: %w", err)
	}
	sender, err := es.NewSender(*sd, defaultLocalPart, displayNameFrom(name, acct.Slug), "")
	if err != nil {
		return err
	}
	sender.IsDefault = true
	if err := c.Senders.Create(ctx, &sender); err != nil && !errors.Is(err, es.ErrSenderExists) {
		return err
	}
	return nil
}

type VerificationSweep struct {
	c *core
}

func (v *VerificationSweep) Execute(ctx context.Context) error {
	return v.Sweep(ctx)
}

func (v *VerificationSweep) Sweep(ctx context.Context) error {
	ctx = es.WithProviderLane(ctx, es.LaneBackground)
	pending, err := v.c.Domains.ListPending(ctx, verificationBatchSize)
	if err != nil {
		return err
	}
	failures := 0
	setups := map[string]bool{}
	for i := range pending {
		d := &pending[i]
		if d.AwaitsSetup() || d.AwaitsRelease() {
			if setups[d.WorkspaceID] {
				continue
			}
			setups[d.WorkspaceID] = true
			if err := v.c.completeSetup(ctx, d.WorkspaceID, false); err != nil {
				failures++
				log.Printf("[emailsending] setup of workspace %s failed: %v", d.WorkspaceID, err)
			}
			continue
		}
		if err := v.c.recheck(ctx, d); err != nil {
			failures++
			log.Printf("[emailsending] verification of %s failed: %v", d.Domain, err)
		}
	}
	if failures > 0 {
		return fmt.Errorf("emailsending: %d of %d domain checks failed", failures, len(pending))
	}
	return nil
}
