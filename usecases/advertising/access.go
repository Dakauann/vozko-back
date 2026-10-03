package advertising

import (
	"context"
	"log"
	"time"

	ads "vozko/domain/advertising"
)

type accountAccess struct {
	accounts ads.AccountRepository
	grants   ads.GrantRepository
	now      func() time.Time
}

func (a accountAccess) open(ctx context.Context, workspaceID, accountID string, use ads.AccountUse) (*ads.AdAccount, string, error) {
	account, err := a.accounts.FindByID(ctx, workspaceID, accountID)
	if err != nil {
		return nil, "", err
	}
	token, err := a.tokenFor(ctx, account, use)
	if err != nil {
		return nil, "", err
	}
	return account, token, nil
}

func (a accountAccess) tokenFor(ctx context.Context, account *ads.AdAccount, use ads.AccountUse) (string, error) {
	if err := account.Allows(use); err != nil {
		return "", err
	}
	grant, err := a.grants.FindByID(ctx, account.GrantID)
	if err != nil {
		return "", err
	}
	if grant.WorkspaceID != account.WorkspaceID {
		return "", ads.ErrGrantNotFound
	}
	if err := grant.Usable(a.now()); err != nil {
		a.markNeedsReconnect(ctx, account)
		return "", err
	}
	if !grant.Allows(use.Scope(), account.MetaAccountID) {
		a.markNeedsReconnect(ctx, account)
		return "", ads.ErrAccountNeedsReconnect
	}
	return grant.AccessToken, nil
}

func (a accountAccess) failed(ctx context.Context, account *ads.AdAccount, err error) error {
	if ads.Classify(err) == ads.FailureReauth {
		a.markNeedsReconnect(ctx, account)
	}
	return err
}

func (a accountAccess) markNeedsReconnect(ctx context.Context, account *ads.AdAccount) {
	if account.Connection == ads.ConnectionNeedsReconnect {
		return
	}
	account.Connection = ads.ConnectionNeedsReconnect
	if err := a.accounts.SetConnection(ctx, account.ID, ads.ConnectionNeedsReconnect); err != nil {
		log.Printf("[ads] could not flag account %s for reconnection: %v", account.ID, err)
	}
}
