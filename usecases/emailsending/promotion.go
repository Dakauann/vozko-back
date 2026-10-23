package emailsending

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	es "vozko/domain/emailsending"
)

type Promotion struct {
	c *core
}

func (p *Promotion) Execute(ctx context.Context) error {
	return p.Sweep(ctx, p.c.Now())
}

func (p *Promotion) Sweep(ctx context.Context, now time.Time) error {
	c := p.c
	ctx = es.WithProviderLane(ctx, es.LaneBackground)
	accounts, err := c.Accounts.ListByMode(ctx, es.ModeProbation)
	if err != nil {
		return err
	}
	failures := 0
	for i := range accounts {
		acct := &accounts[i]
		if acct.Status == es.AccountDormant {
			continue
		}
		outcome, err := c.promotionOutcome(ctx, acct, now)
		if err != nil {
			failures++
			log.Printf("[emailsending] promotion check of workspace %s failed: %v", acct.WorkspaceID, err)
			continue
		}
		if outcome != es.PromotionGranted {
			continue
		}
		if err := c.startPromotion(ctx, acct); err != nil {
			failures++
			log.Printf("[emailsending] promotion of workspace %s failed: %v", acct.WorkspaceID, err)
		}
	}
	if failures > 0 {
		return fmt.Errorf("emailsending: %d promotion checks failed", failures)
	}
	return nil
}

func (c *core) promotionOutcome(ctx context.Context, acct *es.SenderAccount, now time.Time) (es.PromotionOutcome, error) {
	window, err := c.Messages.HealthWindow(ctx, acct.WorkspaceID, es.PromotionWindowStart(now))
	if err != nil {
		return "", err
	}
	if window.FailedTestBatches, err = c.TestBatches.FailedTestBatches(ctx, acct.WorkspaceID, es.PromotionWindowStart(now)); err != nil {
		return "", err
	}
	health, err := c.checkHealth(ctx, acct)
	if err != nil {
		return "", err
	}
	if health.Sending == es.TenantDisabled {
		return es.PromotionNotEligible, nil
	}
	for _, f := range health.Findings {
		if f.Open && f.Impact == es.FindingHigh {
			window.OpenHighFindings++
		}
	}
	return es.Promotion(*acct, window, now), nil
}

func (c *core) startPromotion(ctx context.Context, acct *es.SenderAccount) error {
	trusted, err := c.sharedDomainRow(ctx, acct, es.DomainSharedTrusted)
	if err != nil {
		return err
	}
	if trusted.AwaitsSetup() {
		return c.setupDomain(ctx, acct, trusted)
	}
	if trusted.Status == es.DomainVerified {
		return c.completePromotion(ctx, acct, trusted)
	}
	return nil
}

func (c *core) completePromotion(ctx context.Context, acct *es.SenderAccount, trusted *es.SendingDomain) error {
	probation, err := c.domainOfKind(ctx, acct.WorkspaceID, es.DomainSharedProbation)
	switch {
	case errors.Is(err, es.ErrDomainNotFound):
	case err != nil:
		return err
	default:
		if err := c.Senders.MoveDomain(ctx, acct.WorkspaceID, probation.ID, trusted.ID); err != nil {
			return err
		}
		probation.Retired = true
		if err := c.Domains.Update(ctx, probation); err != nil {
			return err
		}
	}
	if acct.PromotedAt != nil && acct.Mode != es.ModeProbation {
		return nil
	}
	now := c.Now()
	acct.PromotedAt = &now
	if acct.Mode == es.ModeProbation {
		acct.Mode = es.ModeTrusted
	}
	if err := c.Accounts.Update(ctx, acct); err != nil {
		return err
	}
	c.notifyPromoted(ctx, acct)
	return nil
}
