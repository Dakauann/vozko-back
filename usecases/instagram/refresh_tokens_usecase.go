package instagram

import (
	"context"
	"log"
	"time"

	igdomain "vozko/domain/instagram"
	"vozko/infra/meta"
)

const refreshLead = 20 * 24 * time.Hour

const refreshBatchSize = 50

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
	if !account.TokenNeedsRefresh(now, refreshLead) {
		return
	}
	if account.AccessToken == "" {
		uc.markExpired(ctx, account, "stored token is empty")
		return
	}

	grant, err := uc.oauth.RefreshToken(ctx, account.AccessToken)
	if err != nil {
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

type PurgeProcessedEventsUseCase struct {
	events    igdomain.ProcessedEventRepository
	retention time.Duration
}

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
