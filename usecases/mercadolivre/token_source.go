package mercadolivre

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	mldomain "vozko/domain/mercadolivre"
	"vozko/pkg/bounded"
)

const (
	refreshJobHorizon     = time.Hour
	refreshJobConcurrency = 4
	refreshJobMaxAccounts = 5000
	refreshJobPage        = 500
)

type TokenSource struct {
	accounts mldomain.AccountRepository
	oauth    mldomain.OAuthService
	errs     mldomain.ErrorClassifier
	alerts   AccountAlerts
	metrics  Metrics
	now      Clock
}

func NewTokenSource(accounts mldomain.AccountRepository, oauth mldomain.OAuthService, errs mldomain.ErrorClassifier, alerts AccountAlerts, metrics Metrics) *TokenSource {
	if accounts == nil || oauth == nil || errs == nil || alerts == nil || metrics == nil {
		panic("mercadolivre: token source requires accounts, oauth, an error classifier, alerts and metrics")
	}
	return &TokenSource{accounts: accounts, oauth: oauth, errs: errs, alerts: alerts, metrics: metrics, now: utcNow}
}

func (s *TokenSource) Credentials(ctx context.Context, account *mldomain.Account) (mldomain.Credentials, error) {
	if account == nil || account.Status != mldomain.StatusConnected || strings.TrimSpace(account.AccessToken) == "" {
		return mldomain.Credentials{}, mldomain.ErrNeedsReconnect
	}
	now := s.now()
	if !account.TokenNeedsRefresh(now) {
		return account.Credentials(), nil
	}
	if _, err := s.RefreshExpiringBefore(ctx, account.ID, now.Add(mldomain.TokenRefreshLead)); err != nil {
		return mldomain.Credentials{}, err
	}
	fresh, err := s.accounts.FindByID(ctx, account.ID)
	if err != nil {
		return mldomain.Credentials{}, err
	}
	if fresh.Status != mldomain.StatusConnected || strings.TrimSpace(fresh.AccessToken) == "" {
		return mldomain.Credentials{}, mldomain.ErrNeedsReconnect
	}
	if fresh.AccessExpiresAt != nil && now.Before(*fresh.AccessExpiresAt) {
		return fresh.Credentials(), nil
	}
	return mldomain.Credentials{}, ErrTokenRefreshInProgress
}

func (s *TokenSource) RefreshExpiringBefore(ctx context.Context, accountID string, deadline time.Time) (bool, error) {
	var lost *mldomain.Account
	acquired, err := s.accounts.LockForRefresh(ctx, accountID, func(ctx context.Context, locked *mldomain.Account) (mldomain.RefreshOutcome, error) {
		if locked.Status != mldomain.StatusConnected || strings.TrimSpace(locked.RefreshToken) == "" {
			return mldomain.RefreshOutcome{}, nil
		}
		if locked.AccessExpiresAt != nil && !locked.AccessExpiresAt.Before(deadline) {
			return mldomain.RefreshOutcome{}, nil
		}
		grant, err := s.oauth.Refresh(ctx, mldomain.RefreshCredentials{UserID: locked.MLUserID, RefreshToken: locked.RefreshToken})
		if err != nil {
			if s.errs.IsAuth(err) {
				lost = locked
				return reconnectOutcome("Mercado Livre refused the refresh token; the administrator must connect the account again"), nil
			}
			s.metrics.AddMercadoLivreEvent("token", "refresh_failed")
			return mldomain.RefreshOutcome{}, fmt.Errorf("mercadolivre: refresh token of %s: %w", locked.MLUserID, err)
		}
		if grant.UserID != locked.MLUserID {
			lost = locked
			return reconnectOutcome("the refreshed token belongs to another Mercado Livre user"), nil
		}
		if err := grant.Validate(); err != nil {
			lost = locked
			return reconnectOutcome("the refreshed grant is incomplete: " + err.Error()), nil
		}
		now := s.now()
		return mldomain.RefreshOutcome{Tokens: &mldomain.TokenUpdate{
			AccessToken:      grant.AccessToken,
			RefreshToken:     grant.RefreshToken,
			AccessExpiresAt:  now.Add(grant.ExpiresIn),
			RefreshExpiresAt: now.Add(mldomain.RefreshTokenLifetime),
			GrantedScopes:    grant.Scopes,
		}}, nil
	})
	if err != nil {
		return acquired, err
	}
	if lost != nil {
		s.metrics.AddMercadoLivreEvent("token", "needs_reconnect")
		log.Printf("[mercadolivre][token] account %s (%s) needs to be reconnected", lost.ID, lost.MLUserID)
		lost.Status = mldomain.StatusNeedsReconnect
		if alertErr := s.alerts.NeedsReconnect(ctx, lost); alertErr != nil {
			log.Printf("[mercadolivre][token] reconnect alert for %s not published: %v", lost.ID, alertErr)
		}
		return acquired, nil
	}
	if acquired {
		s.metrics.AddMercadoLivreEvent("token", "refreshed")
	}
	return acquired, nil
}

func reconnectOutcome(reason string) mldomain.RefreshOutcome {
	return mldomain.RefreshOutcome{Status: mldomain.StatusNeedsReconnect, StatusReason: reason}
}

type RefreshTokensJob struct {
	accounts mldomain.AccountRepository
	tokens   *TokenSource
	now      Clock
}

func NewRefreshTokensJob(accounts mldomain.AccountRepository, tokens *TokenSource) *RefreshTokensJob {
	if accounts == nil || tokens == nil {
		panic("mercadolivre: refresh job requires accounts and a token source")
	}
	return &RefreshTokensJob{accounts: accounts, tokens: tokens, now: utcNow}
}

func (j *RefreshTokensJob) Execute(ctx context.Context) error {
	deadline := j.now().Add(refreshJobHorizon)
	seen := map[string]bool{}
	for len(seen) < refreshJobMaxAccounts {
		due, err := j.accounts.ListDueForRefresh(ctx, deadline, refreshJobPage)
		if err != nil {
			return err
		}
		var fresh []*mldomain.Account
		for _, a := range due {
			if !seen[a.ID] {
				seen[a.ID] = true
				fresh = append(fresh, a)
			}
		}
		if len(fresh) == 0 {
			return nil
		}
		bounded.ForEach(ctx, fresh, refreshJobConcurrency, func(ctx context.Context, a *mldomain.Account) {
			if _, err := j.tokens.RefreshExpiringBefore(ctx, a.ID, deadline); err != nil {
				log.Printf("[mercadolivre][token] refresh of %s failed, kept the pair: %v", a.ID, err)
			}
		})
		if len(due) < refreshJobPage {
			return nil
		}
	}
	return nil
}

func (s *TokenSource) RecoverFromAuthFailure(ctx context.Context, account *mldomain.Account) (bool, error) {
	if _, err := s.RefreshExpiringBefore(ctx, account.ID, s.now().Add(mldomain.AccessTokenLifetime*2)); err != nil {
		return false, err
	}
	fresh, err := s.accounts.FindByID(ctx, account.ID)
	if err != nil {
		return false, err
	}
	return fresh.Status != mldomain.StatusConnected, nil
}
