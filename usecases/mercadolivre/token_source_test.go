package mercadolivre

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mldomain "vozko/domain/mercadolivre"
)

func newTokenFixture(refresh func(mldomain.RefreshCredentials) (*mldomain.TokenGrant, error)) (*TokenSource, *fakeAccounts, *fakeOAuth, *fakeAlerts, *fakeMetrics) {
	accounts := newFakeAccounts()
	oauth := &fakeOAuth{refresh: refresh}
	alerts := &fakeAlerts{}
	metrics := newFakeMetrics()
	src := NewTokenSource(accounts, oauth, fakeClassifier{}, alerts, metrics)
	src.now = fixedClock
	return src, accounts, oauth, alerts, metrics
}

func rotated(creds mldomain.RefreshCredentials) (*mldomain.TokenGrant, error) {
	return &mldomain.TokenGrant{
		AccessToken: "APP_USR-2", RefreshToken: "TG-2", ExpiresIn: 6 * time.Hour,
		UserID: creds.UserID, Scopes: mldomain.RequiredScopes(),
	}, nil
}

func TestAFreshTokenIsUsedWithoutRefreshing(t *testing.T) {
	src, accounts, oauth, _, _ := newTokenFixture(rotated)
	account := accounts.put(connectedAccount())
	creds, err := src.Credentials(context.Background(), account)
	if err != nil || creds.AccessToken != "APP_USR-1" || oauth.refreshes != 0 {
		t.Fatalf("creds=%+v err=%v refreshes=%d", creds, err, oauth.refreshes)
	}
}

func TestATokenNearExpiryIsRefreshedOnDemand(t *testing.T) {
	src, accounts, oauth, _, metrics := newTokenFixture(rotated)
	account := connectedAccount()
	soon := testNow.Add(5 * time.Minute)
	account.AccessExpiresAt = &soon
	accounts.put(account)

	creds, err := src.Credentials(context.Background(), account)
	if err != nil || creds.AccessToken != "APP_USR-2" || oauth.refreshes != 1 {
		t.Fatalf("creds=%+v err=%v refreshes=%d", creds, err, oauth.refreshes)
	}
	stored := accounts.get(account.ID)
	if stored.RefreshToken != "TG-2" || !stored.AccessExpiresAt.Equal(testNow.Add(6*time.Hour)) {
		t.Fatalf("rotated pair not stored: %+v", stored)
	}
	if metrics.count("token/refreshed") != 1 {
		t.Fatalf("metrics = %v", metrics.events)
	}
}

func TestARefusedGrantNeedsReconnectAndAlerts(t *testing.T) {
	src, accounts, _, alerts, metrics := newTokenFixture(func(mldomain.RefreshCredentials) (*mldomain.TokenGrant, error) {
		return nil, &mlErr{msg: "invalid_grant", auth: true}
	})
	account := connectedAccount()
	expired := testNow.Add(-time.Minute)
	account.AccessExpiresAt = &expired
	accounts.put(account)

	_, err := src.Credentials(context.Background(), account)
	if !errors.Is(err, mldomain.ErrNeedsReconnect) {
		t.Fatalf("err = %v", err)
	}
	stored := accounts.get(account.ID)
	if stored.Status != mldomain.StatusNeedsReconnect || stored.AccessToken != "" || stored.RefreshToken != "" {
		t.Fatalf("the dead grant must be wiped: %+v", stored)
	}
	if len(alerts.alerted) != 1 || metrics.count("token/needs_reconnect") != 1 {
		t.Fatalf("alerts=%v metrics=%v", alerts.alerted, metrics.events)
	}
	if _, err := src.Credentials(context.Background(), stored); !errors.Is(err, mldomain.ErrNeedsReconnect) {
		t.Fatalf("a dead account is never retried: %v", err)
	}
}

func TestANetworkFailureKeepsThePair(t *testing.T) {
	src, accounts, _, alerts, _ := newTokenFixture(func(mldomain.RefreshCredentials) (*mldomain.TokenGrant, error) {
		return nil, &mlErr{msg: "timeout", retryable: true}
	})
	account := connectedAccount()
	expired := testNow.Add(-time.Minute)
	account.AccessExpiresAt = &expired
	accounts.put(account)

	if _, err := src.Credentials(context.Background(), account); err == nil {
		t.Fatal("an expired token with a failed refresh must not be handed out")
	}
	stored := accounts.get(account.ID)
	if stored.Status != mldomain.StatusConnected || stored.RefreshToken != "TG-1" || len(alerts.alerted) != 0 {
		t.Fatalf("a transient failure must keep the pair: %+v", stored)
	}
}

func TestAGrantForAnotherSellerIsARotationLoss(t *testing.T) {
	src, accounts, _, alerts, _ := newTokenFixture(func(mldomain.RefreshCredentials) (*mldomain.TokenGrant, error) {
		return &mldomain.TokenGrant{AccessToken: "x", RefreshToken: "y", ExpiresIn: time.Hour, UserID: "999", Scopes: mldomain.RequiredScopes()}, nil
	})
	account := connectedAccount()
	expired := testNow.Add(-time.Minute)
	account.AccessExpiresAt = &expired
	accounts.put(account)
	if _, err := src.Credentials(context.Background(), account); !errors.Is(err, mldomain.ErrNeedsReconnect) {
		t.Fatalf("err = %v", err)
	}
	if accounts.get(account.ID).Status != mldomain.StatusNeedsReconnect || len(alerts.alerted) != 1 {
		t.Fatal("a grant for another seller must not be stored")
	}
}

func TestConcurrentCallersRefreshOnce(t *testing.T) {
	var calls atomic.Int32
	gate := make(chan struct{})
	src, accounts, _, _, _ := newTokenFixture(func(c mldomain.RefreshCredentials) (*mldomain.TokenGrant, error) {
		calls.Add(1)
		<-gate
		return rotated(c)
	})
	account := connectedAccount()
	soon := testNow.Add(5 * time.Minute)
	account.AccessExpiresAt = &soon
	accounts.put(account)

	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := src.Credentials(context.Background(), account)
			results <- err
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()
	close(results)
	if calls.Load() != 1 {
		t.Fatalf("refreshed %d times", calls.Load())
	}
	for err := range results {
		if err != nil {
			t.Fatalf("a caller got %v; the old token was still valid for minutes", err)
		}
	}
}

func TestRefreshJobRefreshesEverythingExpiringWithinTheHour(t *testing.T) {
	src, accounts, oauth, _, _ := newTokenFixture(rotated)
	soon := testNow.Add(40 * time.Minute)
	later := testNow.Add(3 * time.Hour)
	for i, expiry := range []time.Time{soon, soon, later} {
		a := connectedAccount()
		a.ID = []string{"a", "b", "c"}[i]
		a.MLUserID = []string{"1", "2", "3"}[i]
		e := expiry
		a.AccessExpiresAt = &e
		accounts.put(a)
	}
	job := NewRefreshTokensJob(accounts, src)
	job.now = fixedClock
	if err := job.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if oauth.refreshes != 2 || accounts.get("c").RefreshToken != "TG-1" || accounts.get("a").RefreshToken != "TG-2" {
		t.Fatalf("refreshes=%d", oauth.refreshes)
	}
}

func TestRefreshJobStopsWhenOnlyFailuresRemain(t *testing.T) {
	src, accounts, oauth, _, _ := newTokenFixture(func(mldomain.RefreshCredentials) (*mldomain.TokenGrant, error) {
		return nil, &mlErr{msg: "down", retryable: true}
	})
	soon := testNow.Add(10 * time.Minute)
	a := connectedAccount()
	a.AccessExpiresAt = &soon
	accounts.put(a)
	job := NewRefreshTokensJob(accounts, src)
	job.now = fixedClock
	done := make(chan error, 1)
	go func() { done <- job.Execute(context.Background()) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the job looped on an account that keeps failing")
	}
	if oauth.refreshes != 1 {
		t.Fatalf("refreshes = %d", oauth.refreshes)
	}
}
