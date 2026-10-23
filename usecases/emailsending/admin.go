package emailsending

import (
	"context"
	"strings"
	"time"

	es "vozko/domain/emailsending"
)

const (
	adminPageSize          = 50
	platformPauseReason    = "paused by the platform"
	providerStillPausedMsg = "the provider still keeps sending disabled"
)

type Admin struct {
	c *core
}

type AccountHealthView struct {
	Account         es.SenderAccount
	WorkspaceName   string
	Health          *es.TenantHealth
	HealthError     string
	HealthCheckedAt *time.Time
}

func (a *Admin) List(ctx context.Context, offset, limit int) ([]AccountHealthView, int64, error) {
	if limit <= 0 || limit > adminPageSize {
		limit = adminPageSize
	}
	accounts, total, err := a.c.Accounts.List(ctx, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(accounts))
	for _, acct := range accounts {
		ids = append(ids, acct.WorkspaceID)
	}
	names, err := a.c.Workspaces.WorkspaceNamesByID(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	out := make([]AccountHealthView, 0, len(accounts))
	for _, acct := range accounts {
		view := AccountHealthView{Account: acct, WorkspaceName: names[acct.WorkspaceID]}
		if acct.Status == es.AccountDormant {
			out = append(out, view)
			continue
		}
		if snapshot, ok := a.c.cachedHealth(acct.TenantName); ok {
			view.Health, view.HealthError = snapshot.Health, snapshot.Error
			if !snapshot.CheckedAt.IsZero() {
				checked := snapshot.CheckedAt
				view.HealthCheckedAt = &checked
			}
		}
		out = append(out, view)
	}
	return out, total, nil
}

func (a *Admin) Pause(ctx context.Context, workspaceID, reason string) (*es.SenderAccount, error) {
	acct, err := a.c.liveAccount(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if err := a.c.Provider.SetTenantSending(ctx, acct.TenantName, false); err != nil {
		return nil, providerFailure("pause tenant", err)
	}
	acct.Status = es.AccountPausedByPlatform
	acct.StatusReason = strings.TrimSpace(reason)
	if acct.StatusReason == "" {
		acct.StatusReason = platformPauseReason
	}
	if err := a.c.Accounts.Update(ctx, acct); err != nil {
		return nil, err
	}
	return acct, nil
}

func (a *Admin) Reinstate(ctx context.Context, workspaceID string) (*es.SenderAccount, error) {
	acct, err := a.c.liveAccount(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if err := a.c.Provider.SetTenantSending(ctx, acct.TenantName, true); err != nil {
		return nil, providerFailure("reinstate tenant", err)
	}
	health, err := a.c.checkHealth(ctx, acct)
	if err != nil {
		return nil, err
	}
	if health.Sending == es.TenantDisabled {
		acct.Status, acct.StatusReason = es.AccountPausedByProvider, providerStillPausedMsg
	} else {
		acct.Status, acct.StatusReason = es.AccountActive, ""
	}
	if err := a.c.Accounts.Update(ctx, acct); err != nil {
		return nil, err
	}
	if acct.Status == es.AccountActive {
		a.c.resolve(ctx, workspaceID, tenantPausedKey(workspaceID))
	}
	return acct, nil
}

func (a *Admin) Promote(ctx context.Context, workspaceID string) (*es.SenderAccount, error) {
	c := a.c
	acct, err := c.account(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if acct.Mode != es.ModeProbation {
		return nil, ErrNotInProbation
	}
	trusted, err := c.sharedDomainRow(ctx, acct, es.DomainSharedTrusted)
	if err != nil {
		return nil, err
	}
	if trusted.Status == es.DomainVerified && !trusted.Retired {
		if err := c.completePromotion(ctx, acct, trusted); err != nil {
			return nil, err
		}
		return acct, nil
	}
	if err := c.startSetup(ctx, acct, trusted); err != nil {
		return nil, err
	}
	return acct, nil
}
