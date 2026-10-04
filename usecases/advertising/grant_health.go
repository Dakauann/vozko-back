package advertising

import (
	"context"
	"errors"
	"log"
	"time"

	ads "vozko/domain/advertising"
	fbdomain "vozko/domain/facebook"
	"vozko/domain/metaplatform"
)

const grantHealthBatch = 200

type TokenInspector interface {
	DebugToken(ctx context.Context, token string) (*fbdomain.TokenDebug, error)
}

type GrantHealthUseCase struct {
	grants    ads.GrantRepository
	accounts  ads.AccountRepository
	inspector TokenInspector
	now       func() time.Time
}

var _ metaplatform.AppUserHandler = (*GrantHealthUseCase)(nil)

func NewGrantHealthUseCase(grants ads.GrantRepository, accounts ads.AccountRepository, inspector TokenInspector) *GrantHealthUseCase {
	return &GrantHealthUseCase{grants: grants, accounts: accounts, inspector: inspector, now: func() time.Time { return time.Now().UTC() }}
}

func (uc *GrantHealthUseCase) Execute(ctx context.Context) error {
	grants, err := uc.grants.ListActive(ctx, grantHealthBatch)
	if err != nil {
		return err
	}
	for _, grant := range grants {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		uc.check(ctx, grant)
	}
	return nil
}

func (uc *GrantHealthUseCase) check(ctx context.Context, grant *ads.Grant) {
	debug, err := uc.inspector.DebugToken(ctx, grant.AccessToken)
	if err != nil {
		if failure := fbdomain.Classify(err); failure == fbdomain.FailureReauth || failure == fbdomain.FailureRoleLost {
			uc.logRevoke(ctx, grant, "Meta rejected the token")
			return
		}
		log.Printf("[ads-health] grant %s check skipped: %v", grant.ID, err)
		return
	}
	now := uc.now()
	if !debug.Valid {
		uc.logRevoke(ctx, grant, "the token is no longer valid")
		return
	}
	if debug.ExpiresAt != nil && !now.Before(*debug.ExpiresAt) {
		uc.logRevoke(ctx, grant, "the token expired")
		return
	}
	if err := uc.grants.MarkChecked(ctx, grant.ID, debug.Scopes, debug.GranularScopes, now); err != nil {
		log.Printf("[ads-health] grant %s check not recorded: %v", grant.ID, err)
	}
}

func (uc *GrantHealthUseCase) logRevoke(ctx context.Context, grant *ads.Grant, why string) {
	if err := uc.revoke(ctx, grant); err != nil {
		log.Printf("[ads-health] grant %s not revoked (%s): %v", grant.ID, why, err)
		return
	}
	log.Printf("[ads-health] grant %s revoked: %s; its ad accounts ask to reconnect", grant.ID, why)
}

func (uc *GrantHealthUseCase) revoke(ctx context.Context, grant *ads.Grant) error {
	if err := uc.grants.Revoke(ctx, grant.ID, uc.now()); err != nil {
		return err
	}
	return uc.accounts.ReconnectByGrant(ctx, grant.ID)
}

func (uc *GrantHealthUseCase) RevokeAppUser(ctx context.Context, appScopedUserID string) error {
	grants, err := uc.grants.ListByAppScopedUser(ctx, appScopedUserID)
	if err != nil {
		return err
	}
	var errs []error
	for _, grant := range grants {
		errs = append(errs, uc.revoke(ctx, grant))
	}
	return errors.Join(errs...)
}

func (uc *GrantHealthUseCase) EraseAppUser(ctx context.Context, appScopedUserID string) error {
	grants, err := uc.grants.ListByAppScopedUser(ctx, appScopedUserID)
	if err != nil {
		return err
	}
	var errs []error
	for _, grant := range grants {
		if err := uc.revoke(ctx, grant); err != nil {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, uc.grants.EraseToken(ctx, grant.ID))
	}
	return errors.Join(errs...)
}
