package googleaccount

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/googleaccount"
	"vozko/pkg/bounded"
)

const (
	refreshJobHorizon     = time.Hour
	refreshJobConcurrency = 4
	refreshJobPage        = 500
	refreshJobMaxAccounts = 5000
)

type Clock func() time.Time

func utcNow() time.Time { return time.Now().UTC() }

type TokenSource struct {
	accounts googleaccount.Repository
	oauth    googleaccount.OAuthClient
	errs     googleaccount.ErrorClassifier
	alerts   Alerts
	now      Clock
}

var _ googleaccount.AccessSource = (*TokenSource)(nil)

func NewTokenSource(accounts googleaccount.Repository, oauth googleaccount.OAuthClient, errs googleaccount.ErrorClassifier, alerts Alerts) *TokenSource {
	if accounts == nil || oauth == nil || errs == nil || alerts == nil {
		panic("googleaccount: token source requires accounts, oauth, an error classifier and alerts")
	}
	return &TokenSource{accounts: accounts, oauth: oauth, errs: errs, alerts: alerts, now: utcNow}
}

func (s *TokenSource) Access(ctx context.Context, workspaceID string, product googleaccount.Product) (googleaccount.Access, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return googleaccount.Access{}, googleaccount.ErrWorkspaceRequired
	}
	account, err := s.accounts.FindBound(ctx, workspaceID, product)
	if err != nil {
		return googleaccount.Access{}, err
	}
	if err := account.Usable(); err != nil {
		return googleaccount.Access{}, err
	}
	now := s.now()
	if !account.NeedsRefresh(now) {
		return accessOf(account), nil
	}
	if _, err := s.RefreshExpiringBefore(ctx, account.ID, now.Add(googleaccount.TokenRefreshLead)); err != nil {
		return googleaccount.Access{}, err
	}
	fresh, err := s.accounts.FindByID(ctx, account.ID)
	if err != nil {
		return googleaccount.Access{}, err
	}
	if err := fresh.Usable(); err != nil {
		return googleaccount.Access{}, err
	}
	if now.Before(fresh.AccessExpiresAt) {
		return accessOf(fresh), nil
	}
	return googleaccount.Access{}, googleaccount.ErrRefreshInProgress
}

func accessOf(a *googleaccount.Account) googleaccount.Access {
	return googleaccount.Access{AccountID: a.ID, Email: a.Email, Token: a.AccessToken}
}

func (s *TokenSource) RefreshExpiringBefore(ctx context.Context, accountID string, deadline time.Time) (bool, error) {
	var lost *googleaccount.Account
	acquired, err := s.accounts.LockForRefresh(ctx, accountID, func(ctx context.Context, locked *googleaccount.Account) (googleaccount.RefreshOutcome, error) {
		if locked.Status != googleaccount.StatusConnected || strings.TrimSpace(locked.RefreshToken) == "" {
			return googleaccount.RefreshOutcome{}, nil
		}
		if !locked.AccessExpiresAt.IsZero() && !locked.AccessExpiresAt.Before(deadline) {
			return googleaccount.RefreshOutcome{}, nil
		}
		grant, err := s.oauth.Refresh(ctx, locked.RefreshToken)
		if err != nil {
			if s.errs.IsAuth(err) {
				lost = locked
				return reconnectOutcome("Google recusou o acesso salvo: " + err.Error()), nil
			}
			return googleaccount.RefreshOutcome{}, fmt.Errorf("googleaccount: refresh %s: %w", locked.ID, err)
		}
		if err := grant.Validate(); err != nil {
			lost = locked
			return reconnectOutcome(err.Error()), nil
		}
		refreshToken := grant.RefreshToken
		if strings.TrimSpace(refreshToken) == "" {
			refreshToken = locked.RefreshToken
		}
		return googleaccount.RefreshOutcome{Tokens: &googleaccount.TokenUpdate{
			AccessToken:     grant.AccessToken,
			RefreshToken:    refreshToken,
			AccessExpiresAt: s.now().Add(grant.ExpiresIn),
			Scopes:          googleaccount.MergeScopes(locked.Scopes, grant.Scopes),
		}}, nil
	})
	if err != nil {
		return acquired, err
	}
	if lost != nil {
		log.Printf("[googleaccount] account %s (%s) must be connected again", lost.ID, lost.Email)
		lost.Status = googleaccount.StatusNeedsReconnect
		s.raiseReconnect(ctx, lost)
	}
	return acquired, nil
}

func (s *TokenSource) raiseReconnect(ctx context.Context, account *googleaccount.Account) {
	products, err := boundProducts(ctx, s.accounts, account)
	if err != nil {
		log.Printf("[googleaccount] products of %s unreadable, reconnect alert not published: %v", account.ID, err)
		return
	}
	if err := s.alerts.NeedsReconnect(ctx, account, products); err != nil {
		log.Printf("[googleaccount] reconnect alert of %s not published: %v", account.ID, err)
	}
}

func boundProducts(ctx context.Context, accounts googleaccount.Repository, account *googleaccount.Account) ([]googleaccount.Product, error) {
	bound, err := accounts.ListBound(ctx, account.WorkspaceID)
	if err != nil {
		return nil, err
	}
	var products []googleaccount.Product
	for _, b := range bound {
		if b.Account != nil && b.Account.ID == account.ID {
			products = append(products, b.Product)
		}
	}
	return products, nil
}

func reconnectOutcome(reason string) googleaccount.RefreshOutcome {
	return googleaccount.RefreshOutcome{Status: googleaccount.StatusNeedsReconnect, StatusReason: reason}
}

type RefreshTokensJob struct {
	accounts googleaccount.Repository
	tokens   *TokenSource
	now      Clock
}

func NewRefreshTokensJob(accounts googleaccount.Repository, tokens *TokenSource) *RefreshTokensJob {
	if accounts == nil || tokens == nil {
		panic("googleaccount: refresh job requires accounts and a token source")
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
		var fresh []*googleaccount.Account
		for _, a := range due {
			if !seen[a.ID] {
				seen[a.ID] = true
				fresh = append(fresh, a)
			}
		}
		if len(fresh) == 0 {
			return nil
		}
		bounded.ForEach(ctx, fresh, refreshJobConcurrency, func(ctx context.Context, a *googleaccount.Account) {
			if _, err := j.tokens.RefreshExpiringBefore(ctx, a.ID, deadline); err != nil {
				log.Printf("[googleaccount] refresh of %s failed, kept the pair: %v", a.ID, err)
			}
		})
		if len(due) < refreshJobPage {
			return nil
		}
	}
	return nil
}
