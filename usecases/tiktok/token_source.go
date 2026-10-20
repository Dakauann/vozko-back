package tiktok

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	ttdomain "vozko/domain/tiktok"
	"vozko/pkg/bounded"
)

const (
	refreshJobConcurrency = 4
	refreshJobMaxAccounts = 20000
	refreshJobPage        = 500
)

type TokenSource struct {
	accounts ttdomain.AccountRepository
	oauth    ttdomain.OAuthService
	errs     ttdomain.ErrorClassifier
	alerts   AccountAlerts
	metrics  Metrics
	now      Clock
}

func NewTokenSource(accounts ttdomain.AccountRepository, oauth ttdomain.OAuthService, errs ttdomain.ErrorClassifier, alerts AccountAlerts, metrics Metrics) *TokenSource {
	if accounts == nil || oauth == nil || errs == nil || alerts == nil || metrics == nil {
		panic("tiktok: token source requires accounts, oauth, an error classifier, alerts and metrics")
	}
	return &TokenSource{accounts: accounts, oauth: oauth, errs: errs, alerts: alerts, metrics: metrics, now: utcNow}
}

func (s *TokenSource) Credentials(ctx context.Context, account *ttdomain.Account) (ttdomain.Credentials, error) {
	if account == nil || account.Status != ttdomain.StatusConnected || strings.TrimSpace(account.AccessToken) == "" {
		return ttdomain.Credentials{}, ttdomain.ErrNeedsReconnect
	}
	now := s.now()
	if !account.TokenNeedsRefresh(now) {
		return account.Credentials(), nil
	}
	if _, err := s.RefreshExpiringBefore(ctx, account.ID, now.Add(ttdomain.TokenRefreshLead)); err != nil {
		if account.AccessExpiresAt != nil && now.Before(*account.AccessExpiresAt) {
			log.Printf("[tiktok][token] early refresh of %s failed, using the current token: %v", account.ID, err)
			return account.Credentials(), nil
		}
		return ttdomain.Credentials{}, err
	}
	fresh, err := s.accounts.FindByID(ctx, account.ID)
	if err != nil {
		return ttdomain.Credentials{}, err
	}
	if fresh.Status != ttdomain.StatusConnected || strings.TrimSpace(fresh.AccessToken) == "" {
		return ttdomain.Credentials{}, ttdomain.ErrNeedsReconnect
	}
	if fresh.AccessExpiresAt != nil && now.Before(*fresh.AccessExpiresAt) {
		return fresh.Credentials(), nil
	}
	return ttdomain.Credentials{}, ErrTokenRefreshInProgress
}

func (s *TokenSource) RefreshExpiringBefore(ctx context.Context, accountID string, deadline time.Time) (bool, error) {
	var lost *ttdomain.Account
	acquired, err := s.accounts.LockForRefresh(ctx, accountID, func(ctx context.Context, locked *ttdomain.Account) (ttdomain.RefreshOutcome, error) {
		if locked.Status != ttdomain.StatusConnected || strings.TrimSpace(locked.RefreshToken) == "" {
			return ttdomain.RefreshOutcome{}, nil
		}
		if locked.AccessExpiresAt != nil && !locked.AccessExpiresAt.Before(deadline) {
			return ttdomain.RefreshOutcome{}, nil
		}
		grant, err := s.oauth.Refresh(ctx, locked.RefreshToken)
		if err != nil {
			if s.errs.IsAuth(err) || s.errs.IsPermission(err) {
				lost = locked
				return s.revokedOutcome("TikTok refused the refresh token; the account must be connected again"), nil
			}
			s.metrics.AddTikTokEvent("token", "refresh_failed")
			return ttdomain.RefreshOutcome{}, fmt.Errorf("tiktok: refresh token of %s: %w", locked.ID, err)
		}
		if grant.OpenID != locked.OpenID {
			lost = locked
			return s.revokedOutcome("the refreshed token belongs to another TikTok account"), nil
		}
		if err := grant.Validate(); err != nil {
			lost = locked
			return s.revokedOutcome("the refreshed grant is incomplete: " + err.Error()), nil
		}
		now := s.now()
		refreshExpires := now.Add(grant.RefreshExpiresIn)
		if grant.RefreshExpiresIn <= 0 && locked.RefreshExpiresAt != nil {
			refreshExpires = *locked.RefreshExpiresAt
		}
		return ttdomain.RefreshOutcome{Tokens: &ttdomain.TokenUpdate{
			AccessToken:      grant.AccessToken,
			RefreshToken:     grant.RefreshToken,
			AccessExpiresAt:  now.Add(grant.ExpiresIn),
			RefreshExpiresAt: refreshExpires,
			GrantedScopes:    grant.Scopes,
		}}, nil
	})
	if err != nil {
		return acquired, err
	}
	if lost != nil {
		s.metrics.AddTikTokEvent("token", "revoked")
		log.Printf("[tiktok][token] account %s lost its authorization, purge scheduled", lost.ID)
		lost.Status = ttdomain.StatusRevoked
		lost.PurgeAfter = revokedPurgeAfter(s.now())
		if alertErr := s.alerts.NeedsReconnect(ctx, lost); alertErr != nil {
			log.Printf("[tiktok][token] reconnect alert for %s not published: %v", lost.ID, alertErr)
		}
		return acquired, nil
	}
	if acquired {
		s.metrics.AddTikTokEvent("token", "refreshed")
	}
	return acquired, nil
}

func (s *TokenSource) revokedOutcome(reason string) ttdomain.RefreshOutcome {
	return ttdomain.RefreshOutcome{Status: ttdomain.StatusRevoked, StatusReason: reason, PurgeAfter: revokedPurgeAfter(s.now())}
}

func (s *TokenSource) MarkLost(ctx context.Context, account *ttdomain.Account, reason string) error {
	if err := s.accounts.UpdateStatus(ctx, account.ID, ttdomain.StatusNeedsReconnect, reason); err != nil {
		return err
	}
	s.metrics.AddTikTokEvent("token", "needs_reconnect")
	lost := *account
	lost.Status = ttdomain.StatusNeedsReconnect
	lost.StatusReason = reason
	if err := s.alerts.NeedsReconnect(ctx, &lost); err != nil {
		log.Printf("[tiktok][token] reconnect alert for %s not published: %v", account.ID, err)
	}
	return nil
}

type RefreshTokensJob struct {
	accounts ttdomain.AccountRepository
	tokens   *TokenSource
	now      Clock
}

func NewRefreshTokensJob(accounts ttdomain.AccountRepository, tokens *TokenSource) *RefreshTokensJob {
	if accounts == nil || tokens == nil {
		panic("tiktok: refresh job requires accounts and a token source")
	}
	return &RefreshTokensJob{accounts: accounts, tokens: tokens, now: utcNow}
}

func (j *RefreshTokensJob) Execute(ctx context.Context) error {
	deadline := j.now().Add(ttdomain.TokenRefreshLead)
	seen := map[string]bool{}
	for len(seen) < refreshJobMaxAccounts {
		due, err := j.accounts.ListDueForRefresh(ctx, deadline, refreshJobPage)
		if err != nil {
			return err
		}
		var fresh []*ttdomain.Account
		for _, a := range due {
			if !seen[a.ID] {
				seen[a.ID] = true
				fresh = append(fresh, a)
			}
		}
		if len(fresh) == 0 {
			return nil
		}
		bounded.ForEach(ctx, fresh, refreshJobConcurrency, func(ctx context.Context, a *ttdomain.Account) {
			if _, err := j.tokens.RefreshExpiringBefore(ctx, a.ID, deadline); err != nil {
				log.Printf("[tiktok][token] refresh of %s failed, kept the pair: %v", a.ID, err)
			}
		})
		if len(due) < refreshJobPage {
			return nil
		}
	}
	return nil
}

func revokedPurgeAfter(now time.Time) *time.Time {
	purgeAfter := ttdomain.PurgeScheduleFrom(now)
	return &purgeAfter
}
