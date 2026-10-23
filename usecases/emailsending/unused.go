package emailsending

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	es "vozko/domain/emailsending"
	"vozko/domain/notification"
)

const (
	unusedBatchSize      = 100
	UnusedReleasesPerRun = 50
	EmailOverviewPath    = "/dashboard/email"
)

func unusedReleaseKey(workspaceID string) string { return "email_unused_release:" + workspaceID }

type UnusedRelease struct {
	c *core
}

func (u *UnusedRelease) Execute(ctx context.Context) error {
	return u.Sweep(ctx, u.c.Now())
}

func (u *UnusedRelease) Sweep(ctx context.Context, now time.Time) error {
	c := u.c
	ctx = es.WithProviderLane(ctx, es.LaneBackground)
	activatedBefore := es.UnusedCandidatesActivatedBefore(now)
	after, failures, releases := "", 0, 0
	for {
		batch, err := c.Unused.ListUnusedCandidates(ctx, activatedBefore, after, unusedBatchSize)
		if err != nil {
			return fmt.Errorf("emailsending: unused accounts: %w", err)
		}
		for _, acct := range batch {
			released, err := c.settleUnused(ctx, acct.WorkspaceID, now, releases < UnusedReleasesPerRun)
			if released {
				releases++
			}
			if err != nil {
				failures++
				c.alertOps(ctx, "email unused account release failed", fmt.Sprintf("workspace %s: %v", acct.WorkspaceID, err))
			}
		}
		if len(batch) < unusedBatchSize {
			break
		}
		after = batch[len(batch)-1].ID
	}
	if failures > 0 {
		return fmt.Errorf("emailsending: %d unused accounts could not be settled", failures)
	}
	return nil
}

func (c *core) settleUnused(ctx context.Context, workspaceID string, now time.Time, mayRelease bool) (bool, error) {
	released := false
	err := c.withTenantLock(ctx, workspaceID, func() error {
		acct, err := c.account(ctx, workspaceID)
		if errors.Is(err, es.ErrAccountNotProvisioned) {
			return nil
		}
		if err != nil {
			return err
		}
		used, err := c.Unused.InUse(ctx, workspaceID)
		if err != nil {
			return fmt.Errorf("account use: %w", err)
		}
		switch es.UnusedStepFor(*acct, used, now) {
		case es.UnusedKeep:
			return c.keepUsedAccount(ctx, acct)
		case es.UnusedWarn:
			return c.warnUnused(ctx, acct, now)
		}
		if !mayRelease {
			return c.warnUnused(ctx, acct, now)
		}
		released = true
		return c.releaseUnused(ctx, acct)
	})
	return released, err
}

func (c *core) keepUsedAccount(ctx context.Context, acct *es.SenderAccount) error {
	if acct.UnusedWarnedAt == nil {
		return nil
	}
	acct.UnusedWarnedAt = nil
	if err := c.Accounts.Update(ctx, acct); err != nil {
		return err
	}
	c.resolve(ctx, acct.WorkspaceID, unusedReleaseKey(acct.WorkspaceID))
	return nil
}

func (c *core) warnUnused(ctx context.Context, acct *es.SenderAccount, now time.Time) error {
	if acct.UnusedWarnedAt == nil {
		stamp := now
		acct.UnusedWarnedAt = &stamp
		if err := c.Accounts.Update(ctx, acct); err != nil {
			return err
		}
	}
	address, err := c.defaultAddress(acct.Slug, c.sharedKindOf(acct))
	if err != nil {
		return err
	}
	c.alertUnusedRelease(ctx, acct, address, es.UnusedReleaseAt(acct.CreatedAt, acct.UnusedWarnedAt))
	return nil
}

func (c *core) releaseUnused(ctx context.Context, acct *es.SenderAccount) error {
	domains, err := c.Domains.ListByWorkspace(ctx, acct.WorkspaceID)
	if err != nil {
		return err
	}
	for i := range domains {
		d := &domains[i]
		if d.ReleaseState == es.ReleaseNone && !d.Retired {
			d.Release()
			if err := c.Domains.SetRelease(ctx, d); err != nil {
				return err
			}
		}
	}
	for i := range domains {
		if domains[i].AwaitsRelease() {
			if err := c.releaseDomain(ctx, acct, &domains[i]); err != nil {
				return err
			}
		}
	}
	if err := c.Tenants.RetireTenant(ctx, es.TenantResources{Name: acct.TenantName}); err != nil {
		return providerFailure("retire tenant", err)
	}
	err = c.Unused.Forget(ctx, acct.WorkspaceID, acct.ID)
	if errors.Is(err, es.ErrAccountInUse) {
		return c.rebuildUsedAccount(ctx, acct)
	}
	if err != nil {
		return err
	}
	c.resolve(ctx, acct.WorkspaceID, unusedReleaseKey(acct.WorkspaceID))
	log.Printf("[emailsending] unused email account of workspace %s released (%s)", acct.WorkspaceID, acct.Slug)
	return nil
}

func (c *core) rebuildUsedAccount(ctx context.Context, acct *es.SenderAccount) error {
	c.alertOps(ctx, "email account used during its release", fmt.Sprintf("workspace %s sent while its unused account was being released; its domains are set up again", acct.WorkspaceID))
	if _, err := c.settleSharedDomain(ctx, acct); err != nil {
		return err
	}
	domains, err := c.Domains.ListByWorkspace(ctx, acct.WorkspaceID)
	if err != nil {
		return err
	}
	for i := range domains {
		d := &domains[i]
		if d.Kind == es.DomainOwn && d.ReleaseState != es.ReleaseNone {
			d.Reinstate()
			if err := c.Domains.SetRelease(ctx, d); err != nil {
				return err
			}
			if err := c.Domains.Update(ctx, d); err != nil {
				return err
			}
		}
	}
	acct.Status, acct.StatusReason, acct.UnusedWarnedAt = es.AccountDormant, "", nil
	if err := c.Accounts.Update(ctx, acct); err != nil {
		return err
	}
	c.resolve(ctx, acct.WorkspaceID, unusedReleaseKey(acct.WorkspaceID))
	pending, err := c.domainsAwaitingSetup(ctx, acct.WorkspaceID)
	if err != nil {
		return err
	}
	return c.startSetup(ctx, acct, pending...)
}

func (c *core) alertUnusedRelease(ctx context.Context, acct *es.SenderAccount, address string, releaseAt time.Time) {
	day := es.UnusedReleaseDay(releaseAt)
	c.publish(ctx, notification.Draft{
		WorkspaceID: acct.WorkspaceID,
		Category:    notification.CategoryChannels,
		Severity:    notification.SeverityWarning,
		Title:       "O envio de e-mails será desativado em " + day,
		Body: "O envio de e-mails foi ativado há mais de três semanas e nenhum e-mail saiu até agora. Em " + day +
			" a configuração será removida e o endereço " + address + " fica livre. Para manter, envie qualquer e-mail antes dessa data: uma campanha, um teste ou uma resposta. Depois disso, é só ativar de novo quando quiser.",
		ActionLabel: "Abrir e-mail",
		ActionURL:   EmailOverviewPath,
		EpisodeKey:  unusedReleaseKey(acct.WorkspaceID),
	})
}
