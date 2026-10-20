package tiktok

import (
	"context"
	"errors"
	"testing"
	"time"

	ttdomain "vozko/domain/tiktok"
)

func newTokenSourceForTest() (*TokenSource, *fakeAccounts, *fakeServices, *fakeAlerts, *fakeMetrics) {
	accounts := newFakeAccounts()
	svc := newFakeServices()
	alerts := &fakeAlerts{}
	metrics := &fakeMetrics{}
	ts := NewTokenSource(accounts, svc, fakeClassifier{}, alerts, metrics)
	ts.now = fixedClock
	return ts, accounts, svc, alerts, metrics
}

func TestCredentialsReturnsTheCurrentTokenWhileFresh(t *testing.T) {
	ts, accounts, _, _, _ := newTokenSourceForTest()
	a := accounts.put(connectedAccount())
	creds, err := ts.Credentials(context.Background(), a)
	if err != nil || creds.AccessToken != "act.1" || creds.OpenID != a.OpenID {
		t.Fatalf("creds %+v err %v", creds, err)
	}
	if accounts.refreshN != 0 {
		t.Error("a fresh token must not be refreshed")
	}
}

func TestCredentialsRefreshesATokenAboutToExpire(t *testing.T) {
	ts, accounts, svc, _, metrics := newTokenSourceForTest()
	a := connectedAccount()
	soon := testNow.Add(time.Hour)
	a.AccessExpiresAt = &soon
	accounts.put(a)
	svc.refreshed = ttdomain.Grant{OpenID: a.OpenID, AccessToken: "act.2", RefreshToken: "rft.2", ExpiresIn: 24 * time.Hour, RefreshExpiresIn: 300 * 24 * time.Hour, Scopes: []string{ttdomain.ScopeVideoList}}
	creds, err := ts.Credentials(context.Background(), a)
	if err != nil || creds.AccessToken != "act.2" {
		t.Fatalf("creds %+v err %v", creds, err)
	}
	stored := accounts.get(a.ID)
	if stored.RefreshToken != "rft.2" || !stored.AccessExpiresAt.Equal(testNow.Add(24*time.Hour)) || !stored.RefreshExpiresAt.Equal(testNow.Add(300*24*time.Hour)) {
		t.Fatalf("stored %+v", stored)
	}
	if metrics.count("token:refreshed") != 1 {
		t.Errorf("metrics = %v", metrics.events)
	}
}

func TestARefusedRefreshRevokesTheAccountSchedulesThePurgeAndAlerts(t *testing.T) {
	ts, accounts, svc, alerts, _ := newTokenSourceForTest()
	a := connectedAccount()
	past := testNow.Add(-time.Minute)
	a.AccessExpiresAt = &past
	accounts.put(a)
	svc.refreshErr = codedError{auth: true, refusal: true}
	if _, err := ts.Credentials(context.Background(), a); !errors.Is(err, ttdomain.ErrNeedsReconnect) {
		t.Fatalf("err = %v", err)
	}
	stored := accounts.get(a.ID)
	if stored.Status != ttdomain.StatusRevoked || stored.AccessToken != "" || stored.RefreshToken != "" || stored.PurgeAfter == nil || !stored.PurgeAfter.Equal(testNow.Add(ttdomain.PurgeGracePeriod)) {
		t.Fatalf("stored %+v", stored)
	}
	if len(alerts.reconnect) != 1 {
		t.Errorf("alerts = %+v", alerts)
	}
}

func TestATransientRefreshFailureKeepsThePair(t *testing.T) {
	ts, accounts, svc, alerts, _ := newTokenSourceForTest()
	a := connectedAccount()
	soon := testNow.Add(time.Hour)
	a.AccessExpiresAt = &soon
	accounts.put(a)
	svc.refreshErr = codedError{retry: true}
	if _, err := ts.RefreshExpiringBefore(context.Background(), a.ID, testNow.Add(ttdomain.TokenRefreshLead)); err == nil {
		t.Fatal("a transient failure is reported to the caller")
	}
	stored := accounts.get(a.ID)
	if stored.Status != ttdomain.StatusConnected || stored.RefreshToken != "rft.1" || len(alerts.reconnect) != 0 {
		t.Fatalf("stored %+v alerts %+v", stored, alerts)
	}
}

func TestARefreshForAnotherAccountIsRefused(t *testing.T) {
	ts, accounts, svc, _, _ := newTokenSourceForTest()
	a := connectedAccount()
	soon := testNow.Add(time.Hour)
	a.AccessExpiresAt = &soon
	accounts.put(a)
	svc.refreshed = ttdomain.Grant{OpenID: "_000someoneElse", AccessToken: "act.2", RefreshToken: "rft.2", ExpiresIn: 24 * time.Hour}
	if _, err := ts.RefreshExpiringBefore(context.Background(), a.ID, testNow.Add(ttdomain.TokenRefreshLead)); err != nil {
		t.Fatal(err)
	}
	if stored := accounts.get(a.ID); stored.Status != ttdomain.StatusRevoked || stored.AccessToken == "act.2" {
		t.Fatalf("a token of another account was kept: %+v", stored)
	}
}

func TestCredentialsRefuseAnAccountThatIsNotConnected(t *testing.T) {
	ts, accounts, _, _, _ := newTokenSourceForTest()
	a := connectedAccount()
	a.Status = ttdomain.StatusNeedsReconnect
	accounts.put(a)
	if _, err := ts.Credentials(context.Background(), a); !errors.Is(err, ttdomain.ErrNeedsReconnect) {
		t.Fatalf("err = %v", err)
	}
}

func TestRefreshJobRefreshesEveryDueAccount(t *testing.T) {
	ts, accounts, svc, _, _ := newTokenSourceForTest()
	soon := testNow.Add(time.Hour)
	var ids []string
	for i := 0; i < 3; i++ {
		a := connectedAccount()
		a.ID = ""
		a.OpenID = "_000same"
		a.AccessExpiresAt = &soon
		ids = append(ids, accounts.put(a).ID)
	}
	fresh := connectedAccount()
	accounts.put(fresh)
	svc.refreshed = ttdomain.Grant{OpenID: "_000same", AccessToken: "act.2", RefreshToken: "rft.2", ExpiresIn: 24 * time.Hour}
	job := NewRefreshTokensJob(accounts, ts)
	job.now = fixedClock
	if err := job.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if accounts.get(id).AccessToken != "act.2" {
			t.Errorf("account %s was not refreshed", id)
		}
	}
	if accounts.get(fresh.ID).AccessToken != "act.1" {
		t.Error("a fresh account was refreshed")
	}
}
