package emailsending

import (
	"context"
	"fmt"
	"log"
	"time"

	es "vozko/domain/emailsending"
)

type LiveCampaigns interface {
	HasLiveCampaign(ctx context.Context, workspaceID string) (bool, error)
}

func tenantLockKey(workspaceID string) string { return "email_tenant_lifecycle:" + workspaceID }

type Dormancy struct {
	c *core
}

func (d *Dormancy) Execute(ctx context.Context) error {
	return d.Sweep(ctx, d.c.Now())
}

func (d *Dormancy) Sweep(ctx context.Context, now time.Time) error {
	c := d.c
	ctx = es.WithProviderLane(ctx, es.LaneAccount)
	accounts, err := c.Accounts.ListByStatus(ctx, es.AccountActive)
	if err != nil {
		return err
	}
	failures := 0
	for _, acct := range accounts {
		if err := c.retireIfIdle(ctx, acct.WorkspaceID, now); err != nil {
			failures++
			c.alertOps(ctx, "email tenant retirement failed", fmt.Sprintf("workspace %s: %v", acct.WorkspaceID, err))
		}
	}
	if failures > 0 {
		return fmt.Errorf("emailsending: %d idle tenants could not be retired", failures)
	}
	return nil
}

type Reactivation struct {
	c *core
}

func (r *Reactivation) Reactivate(ctx context.Context, workspaceID string) error {
	_, err := r.c.liveAccount(ctx, workspaceID)
	return err
}

func (c *core) liveAccount(ctx context.Context, workspaceID string) (*es.SenderAccount, error) {
	acct, err := c.account(ctx, workspaceID)
	if err != nil || acct.Status != es.AccountDormant {
		return acct, err
	}
	return c.reactivate(ctx, workspaceID)
}

func (c *core) reactivate(ctx context.Context, workspaceID string) (*es.SenderAccount, error) {
	var revived *es.SenderAccount
	err := c.withTenantLock(ctx, workspaceID, func() error {
		acct, err := c.account(ctx, workspaceID)
		if err != nil {
			return err
		}
		if acct.Status == es.AccountDormant {
			if err := c.wake(ctx, acct); err != nil {
				return err
			}
		}
		revived = acct
		return nil
	})
	if err != nil {
		c.alertOps(ctx, "email tenant reactivation failed", fmt.Sprintf("workspace %s: %v", workspaceID, err))
		return nil, fmt.Errorf("%w: %v", es.ErrAccountReactivation, err)
	}
	return revived, nil
}

func (c *core) wake(ctx context.Context, acct *es.SenderAccount) error {
	tenant, err := c.restoreTenant(ctx, acct)
	if err != nil {
		return err
	}
	acct.Status, acct.StatusReason = es.AccountActive, ""
	if tenant.Ref != "" {
		acct.TenantRef = tenant.Ref
	}
	return c.Accounts.Update(ctx, acct)
}

func (c *core) retireIfIdle(ctx context.Context, workspaceID string, now time.Time) error {
	return c.withTenantLock(ctx, workspaceID, func() error {
		acct, err := c.account(ctx, workspaceID)
		if err != nil {
			return err
		}
		if acct.Status != es.AccountActive {
			return nil
		}
		idle, err := c.idle(ctx, acct, now)
		if err != nil || !idle {
			return err
		}
		return c.retire(ctx, acct)
	})
}

func (c *core) idle(ctx context.Context, acct *es.SenderAccount, now time.Time) (bool, error) {
	lastSent, err := c.Activity.LastSentAt(ctx, acct.WorkspaceID)
	if err != nil {
		return false, fmt.Errorf("last send: %w", err)
	}
	live, err := c.Campaigns.HasLiveCampaign(ctx, acct.WorkspaceID)
	if err != nil {
		return false, fmt.Errorf("live campaigns: %w", err)
	}
	return es.Dormant(acct.LastActiveAt(lastSent), now, live), nil
}

func (c *core) retire(ctx context.Context, acct *es.SenderAccount) error {
	resources, err := c.tenantResources(ctx, acct)
	if err != nil {
		return err
	}
	if err := c.Tenants.RetireTenant(ctx, resources); err != nil {
		return c.undoRetirement(ctx, acct, providerFailure("retire tenant", err))
	}
	acct.Status, acct.StatusReason = es.AccountDormant, ""
	if err := c.Accounts.Update(ctx, acct); err != nil {
		acct.Status = es.AccountActive
		return c.undoRetirement(ctx, acct, err)
	}
	return nil
}

func (c *core) undoRetirement(ctx context.Context, acct *es.SenderAccount, cause error) error {
	tenant, err := c.restoreTenant(ctx, acct)
	if err != nil {
		acct.Status, acct.StatusReason = es.AccountDormant, ""
		if updateErr := c.Accounts.Update(ctx, acct); updateErr != nil {
			return fmt.Errorf("%w; restoring the tenant failed (%v) and the account could not be marked dormant: %v", cause, err, updateErr)
		}
		return fmt.Errorf("%w; restoring the tenant failed, the account is dormant so the next send rebuilds it: %v", cause, err)
	}
	if tenant.Ref != "" && tenant.Ref != acct.TenantRef {
		acct.TenantRef = tenant.Ref
		if updateErr := c.Accounts.Update(ctx, acct); updateErr != nil {
			log.Printf("[emailsending] tenant of workspace %s restored but its new ref was not stored: %v", acct.WorkspaceID, updateErr)
		}
	}
	return cause
}

func (c *core) restoreTenant(ctx context.Context, acct *es.SenderAccount) (es.Tenant, error) {
	resources, err := c.tenantResources(ctx, acct)
	if err != nil {
		return es.Tenant{}, err
	}
	tenant, err := c.Tenants.ReviveTenant(ctx, resources)
	if err != nil {
		return es.Tenant{}, providerFailure("revive tenant", err)
	}
	return tenant, nil
}

func (c *core) tenantResources(ctx context.Context, acct *es.SenderAccount) (es.TenantResources, error) {
	domains, err := c.Domains.ListByWorkspace(ctx, acct.WorkspaceID)
	if err != nil {
		return es.TenantResources{}, err
	}
	resources := es.TenantResources{Name: acct.TenantName}
	for _, d := range domains {
		if d.HasProviderIdentity() {
			resources.Identities = append(resources.Identities, d.Domain)
		}
	}
	return resources, nil
}

func (c *core) withTenantLock(ctx context.Context, workspaceID string, fn func() error) error {
	release, err := c.Locks.Acquire(ctx, tenantLockKey(workspaceID))
	if err != nil {
		return fmt.Errorf("tenant lock: %w", err)
	}
	defer func() {
		released, err := release()
		switch {
		case err != nil:
			log.Printf("[emailsending] tenant lock of workspace %s was not released: %v", workspaceID, err)
		case !released:
			log.Printf("[emailsending] tenant lock of workspace %s expired before its release", workspaceID)
		}
	}()
	return fn()
}

func (c *core) alertOps(ctx context.Context, subject, detail string) {
	log.Printf("[emailsending] %s: %s", subject, detail)
	if err := c.Ops.Alert(ctx, subject, detail); err != nil {
		log.Printf("[emailsending] ops alert %q failed: %v", subject, err)
	}
}
