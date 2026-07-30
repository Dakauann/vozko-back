package instagram

import (
	"context"
	"log"
	"time"

	igdomain "vozko/domain/instagram"
	"vozko/infra/meta"
)

// refreshLead is how far ahead of expiry we refresh.
//
// Long-lived Instagram tokens last 60 days, a refresh is rejected on a token
// younger than 24 hours, and a token left unused for 60 days dies permanently
// with no recovery except full re-auth. Refreshing 20 days out gives ample room
// for repeated failures before a tenant is actually locked out.
const refreshLead = 20 * 24 * time.Hour

// refreshBatchSize bounds one cron pass.
const refreshBatchSize = 50

// RefreshTokensUseCase keeps long-lived tokens alive.
type RefreshTokensUseCase struct {
	accounts igdomain.AccountRepository
	oauth    igdomain.OAuthService
}

func NewRefreshTokensUseCase(
	accounts igdomain.AccountRepository,
	oauth igdomain.OAuthService,
) *RefreshTokensUseCase {
	return &RefreshTokensUseCase{accounts: accounts, oauth: oauth}
}

// Execute refreshes every account due for it.
//
// One tenant's failure never aborts the pass: each account is handled
// independently so a single revoked token cannot starve everyone else's refresh.
func (uc *RefreshTokensUseCase) Execute(ctx context.Context) error {
	now := time.Now().UTC()
	cutoff := now.Add(refreshLead)

	accounts, err := uc.accounts.ListDueForTokenRefresh(ctx, cutoff, refreshBatchSize)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		return nil
	}

	log.Printf("[instagram] refreshing %d token(s)", len(accounts))
	for _, account := range accounts {
		uc.refreshOne(ctx, account, now)
	}
	return nil
}

func (uc *RefreshTokensUseCase) refreshOne(ctx context.Context, account *igdomain.Account, now time.Time) {
	// Re-check the 24h floor in the domain as well as in SQL, so a caller that
	// hands us an arbitrary account cannot trip an upstream rejection.
	if !account.TokenNeedsRefresh(now, refreshLead) {
		return
	}
	if account.AccessToken == "" {
		uc.markExpired(ctx, account, "stored token is empty")
		return
	}

	grant, err := uc.oauth.RefreshToken(ctx, account.AccessToken)
	if err != nil {
		// A dead token cannot be recovered by retrying: the tenant has to
		// reconnect, so mark it and surface the state in the UI.
		if meta.IsReauthRequired(err) {
			uc.markExpired(ctx, account, "token rejected by Instagram; reconnect required")
			return
		}
		log.Printf("[instagram] token refresh failed account=%s (will retry): %v", account.IGUserID, err)
		return
	}

	expiresAt := now.Add(grant.ExpiresIn)
	if err := uc.accounts.UpdateToken(ctx, account.ID, grant.AccessToken, expiresAt, now); err != nil {
		log.Printf("[instagram] persist refreshed token failed account=%s: %v", account.IGUserID, err)
		return
	}
	log.Printf("[instagram] refreshed token account=%s expires=%s", account.IGUserID, expiresAt.Format(time.RFC3339))
}

func (uc *RefreshTokensUseCase) markExpired(ctx context.Context, account *igdomain.Account, reason string) {
	if !account.Status.CanTransitionTo(igdomain.StatusTokenExpired) {
		return
	}
	if err := uc.accounts.UpdateStatus(ctx, account.ID, igdomain.StatusTokenExpired, reason); err != nil {
		log.Printf("[instagram] mark token expired failed account=%s: %v", account.IGUserID, err)
		return
	}
	log.Printf("[instagram] account=%s marked TOKEN_EXPIRED: %s", account.IGUserID, reason)
}

// PurgeProcessedEventsUseCase trims the durable webhook dedup table.
type PurgeProcessedEventsUseCase struct {
	events    igdomain.ProcessedEventRepository
	retention time.Duration
}

// NewPurgeProcessedEventsUseCase builds the purge job. Retention only needs to
// outlive Meta's redelivery horizon.
func NewPurgeProcessedEventsUseCase(
	events igdomain.ProcessedEventRepository,
	retention time.Duration,
) *PurgeProcessedEventsUseCase {
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	return &PurgeProcessedEventsUseCase{events: events, retention: retention}
}

func (uc *PurgeProcessedEventsUseCase) Execute(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-uc.retention)
	deleted, err := uc.events.PurgeOlderThan(ctx, cutoff)
	if err != nil {
		return err
	}
	if deleted > 0 {
		log.Printf("[instagram] purged %d processed webhook event(s)", deleted)
	}
	return nil
}
