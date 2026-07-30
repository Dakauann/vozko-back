package instagram

import (
	"context"
	"errors"
	"testing"
	"time"

	igdomain "vozko/domain/instagram"
	"vozko/infra/meta"
)

func dueAccount(id string) *igdomain.Account {
	now := time.Now().UTC()
	expires := now.Add(10 * 24 * time.Hour) // inside the 20-day refresh lead
	refreshed := now.Add(-48 * time.Hour)   // past the 24h floor
	return &igdomain.Account{
		ID:               id,
		WorkspaceID:      "ws-1",
		IGUserID:         "ig-" + id,
		Username:         id,
		AccessToken:      "token-" + id,
		Status:           igdomain.StatusConnected,
		TokenExpiresAt:   &expires,
		TokenRefreshedAt: &refreshed,
		GrantedScopes:    []string{igdomain.ScopeBasic, igdomain.ScopeManageMessages},
	}
}

func TestRefreshTokens_RefreshesDueAccounts(t *testing.T) {
	accounts := &fakeAccountRepo{
		ListDueFn: func(context.Context, time.Time, int) ([]*igdomain.Account, error) {
			return []*igdomain.Account{dueAccount("a"), dueAccount("b")}, nil
		},
	}
	oauth := &fakeOAuthService{}

	if err := NewRefreshTokensUseCase(accounts, oauth).Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if oauth.Refreshes != 2 {
		t.Errorf("refresh calls = %d, want 2", oauth.Refreshes)
	}
	if accounts.TokenUpdates != 2 {
		t.Errorf("token updates = %d, want 2", accounts.TokenUpdates)
	}
}

// TestRefreshTokens_SkipsTokensYoungerThan24h: Instagram rejects a refresh on a
// token younger than 24 hours, so attempting one wastes a call and logs a
// misleading error.
func TestRefreshTokens_SkipsTokensYoungerThan24h(t *testing.T) {
	account := dueAccount("fresh")
	justRefreshed := time.Now().UTC().Add(-time.Hour)
	account.TokenRefreshedAt = &justRefreshed

	accounts := &fakeAccountRepo{
		ListDueFn: func(context.Context, time.Time, int) ([]*igdomain.Account, error) {
			return []*igdomain.Account{account}, nil
		},
	}
	oauth := &fakeOAuthService{}

	if err := NewRefreshTokensUseCase(accounts, oauth).Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if oauth.Refreshes != 0 {
		t.Errorf("refresh calls = %d, want 0 (inside the 24h floor)", oauth.Refreshes)
	}
}

// TestRefreshTokens_DeadTokenMarksReconnectRequired: a rejected token cannot be
// recovered by retrying, so the account is flagged and the UI can prompt a reconnect.
func TestRefreshTokens_DeadTokenMarksReconnectRequired(t *testing.T) {
	accounts := &fakeAccountRepo{
		ListDueFn: func(context.Context, time.Time, int) ([]*igdomain.Account, error) {
			return []*igdomain.Account{dueAccount("dead")}, nil
		},
	}
	oauth := &fakeOAuthService{
		RefreshFn: func(context.Context, string) (*igdomain.TokenGrant, error) {
			return nil, &meta.Error{Code: meta.CodeAccessTokenError, Message: "invalid token"}
		},
	}

	if err := NewRefreshTokensUseCase(accounts, oauth).Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(accounts.StatusUpdates) != 1 || accounts.StatusUpdates[0] != igdomain.StatusTokenExpired {
		t.Fatalf("status updates = %v, want one TOKEN_EXPIRED", accounts.StatusUpdates)
	}
	if accounts.TokenUpdates != 0 {
		t.Error("a dead token should not be written back")
	}
}

// TestRefreshTokens_TransientFailureIsRetriedLater: a transient error must NOT mark
// the account expired — that would send a working tenant to a reconnect screen.
func TestRefreshTokens_TransientFailureIsRetriedLater(t *testing.T) {
	accounts := &fakeAccountRepo{
		ListDueFn: func(context.Context, time.Time, int) ([]*igdomain.Account, error) {
			return []*igdomain.Account{dueAccount("flaky")}, nil
		},
	}
	oauth := &fakeOAuthService{
		RefreshFn: func(context.Context, string) (*igdomain.TokenGrant, error) {
			return nil, &meta.Error{Code: meta.CodeAPIService, IsTransient: true, Message: "try again"}
		},
	}

	if err := NewRefreshTokensUseCase(accounts, oauth).Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(accounts.StatusUpdates) != 0 {
		t.Errorf("status updates = %v, want none for a transient failure", accounts.StatusUpdates)
	}
}

// TestRefreshTokens_OneTenantFailureDoesNotAbortTheRest is the per-tenant isolation
// property: a single revoked token must not starve every other account's refresh.
func TestRefreshTokens_OneTenantFailureDoesNotAbortTheRest(t *testing.T) {
	accounts := &fakeAccountRepo{
		ListDueFn: func(context.Context, time.Time, int) ([]*igdomain.Account, error) {
			return []*igdomain.Account{dueAccount("dead"), dueAccount("healthy")}, nil
		},
	}
	oauth := &fakeOAuthService{
		RefreshFn: func(_ context.Context, token string) (*igdomain.TokenGrant, error) {
			if token == "token-dead" {
				return nil, &meta.Error{Code: meta.CodeAccessTokenError}
			}
			return &igdomain.TokenGrant{AccessToken: "refreshed", ExpiresIn: 60 * 24 * time.Hour}, nil
		},
	}

	if err := NewRefreshTokensUseCase(accounts, oauth).Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if accounts.TokenUpdates != 1 {
		t.Errorf("token updates = %d, want 1 (the healthy account)", accounts.TokenUpdates)
	}
	if len(accounts.StatusUpdates) != 1 {
		t.Errorf("status updates = %v, want one (the dead account)", accounts.StatusUpdates)
	}
}

func TestRefreshTokens_NoWorkIsNotAnError(t *testing.T) {
	accounts := &fakeAccountRepo{}
	oauth := &fakeOAuthService{}

	if err := NewRefreshTokensUseCase(accounts, oauth).Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if oauth.Refreshes != 0 {
		t.Errorf("refresh calls = %d, want 0", oauth.Refreshes)
	}
}

func TestRefreshTokens_PropagatesRepositoryFailure(t *testing.T) {
	sentinel := errors.New("db down")
	accounts := &fakeAccountRepo{
		ListDueFn: func(context.Context, time.Time, int) ([]*igdomain.Account, error) {
			return nil, sentinel
		},
	}

	err := NewRefreshTokensUseCase(accounts, &fakeOAuthService{}).Execute(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the repository error", err)
	}
}

// TestAccountStatus_TransitionGuard covers the lifecycle guard the WhatsApp phone
// entity lacks entirely (there, every mutation is a bare field assignment).
func TestAccountStatus_TransitionGuard(t *testing.T) {
	cases := []struct {
		from, to igdomain.Status
		allowed  bool
	}{
		{igdomain.StatusPending, igdomain.StatusConnected, true},
		{igdomain.StatusConnected, igdomain.StatusTokenExpired, true},
		{igdomain.StatusTokenExpired, igdomain.StatusConnected, true},
		{igdomain.StatusRevoked, igdomain.StatusConnected, true},
		{igdomain.StatusConnected, igdomain.StatusConnected, true},
		// A pending account has no token yet, so it cannot expire one.
		{igdomain.StatusPending, igdomain.StatusTokenExpired, false},
		{igdomain.StatusRevoked, igdomain.StatusSuspended, false},
		{igdomain.StatusConnected, igdomain.Status("NONSENSE"), false},
	}

	for _, c := range cases {
		if got := c.from.CanTransitionTo(c.to); got != c.allowed {
			t.Errorf("%s -> %s = %t, want %t", c.from, c.to, got, c.allowed)
		}
	}
}

// TestAccount_TokenNeedsRefresh exercises both boundaries at once: the expiry lead
// and the 24h floor.
func TestAccount_TokenNeedsRefresh(t *testing.T) {
	now := time.Now().UTC()
	lead := 20 * 24 * time.Hour

	t.Run("due", func(t *testing.T) {
		if !dueAccount("a").TokenNeedsRefresh(now, lead) {
			t.Error("an account inside the lead window and past the floor should be due")
		}
	})

	t.Run("not near expiry", func(t *testing.T) {
		a := dueAccount("a")
		far := now.Add(50 * 24 * time.Hour)
		a.TokenExpiresAt = &far
		if a.TokenNeedsRefresh(now, lead) {
			t.Error("an account far from expiry should not be due")
		}
	})

	t.Run("inside 24h floor", func(t *testing.T) {
		a := dueAccount("a")
		recent := now.Add(-time.Hour)
		a.TokenRefreshedAt = &recent
		if a.TokenNeedsRefresh(now, lead) {
			t.Error("a token refreshed an hour ago cannot be refreshed again")
		}
	})

	t.Run("not connected", func(t *testing.T) {
		a := dueAccount("a")
		a.Status = igdomain.StatusRevoked
		if a.TokenNeedsRefresh(now, lead) {
			t.Error("a revoked account should not be refreshed")
		}
	})
}
