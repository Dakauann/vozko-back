package googleaccount

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"testing"
	"time"

	"vozko/domain/googleaccount"
	"vozko/usecases/shared/oauthstate"
)

const ws = "8a2f4f5e-4b0c-4b47-9b0e-3b9cf1c4a001"

type world struct {
	repo      *fakeRepo
	oauth     *fakeOAuth
	alerts    *fakeAlerts
	hooks     *fakeHooks
	states    *oauthstate.SealedIssuer
	connector *Connector
	tokens    *TokenSource
	off       *Disconnector
}

func newWorld() *world {
	w := &world{repo: newFakeRepo(), oauth: &fakeOAuth{}, alerts: &fakeAlerts{}, hooks: &fakeHooks{}, states: newSealedIssuer()}
	hooks := map[googleaccount.Product]ProductHooks{googleaccount.ProductCalendar: w.hooks}
	w.connector = NewConnector(ConnectDeps{Accounts: w.repo, OAuth: w.oauth, States: w.states, Alerts: w.alerts, Hooks: hooks})
	w.tokens = NewTokenSource(w.repo, w.oauth, fakeClassifier{}, w.alerts)
	w.off = NewDisconnector(w.repo, w.oauth, w.alerts, hooks)
	return w
}

func (w *world) grantFor(email string, scopes ...string) {
	w.oauth.grant = googleaccount.Grant{AccessToken: "access-" + email, RefreshToken: "refresh-" + email, ExpiresIn: time.Hour, Scopes: scopes}
	w.oauth.profile = googleaccount.Profile{Email: email}
}

func (w *world) connect(t *testing.T, workspaceID string, product googleaccount.Product) (*CompleteOutput, error) {
	t.Helper()
	target, err := w.connector.Start(context.Background(), StartInput{WorkspaceID: workspaceID, UserID: "user-1", Product: product, Popup: true})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	parsed, _ := url.Parse(target)
	return w.connector.Complete(context.Background(), CompleteInput{Code: "code", State: parsed.Query().Get("state")})
}

func TestConnectBindsTheProductAndRunsItsFollowUp(t *testing.T) {
	w := newWorld()
	w.grantFor("ops@example.com", googleaccount.ProductCalendar.Scopes()...)
	out, err := w.connect(t, ws, googleaccount.ProductCalendar)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out.Product != googleaccount.ProductCalendar || !out.Popup || out.Account == nil {
		t.Fatalf("out = %+v", out)
	}
	access, err := w.tokens.Access(context.Background(), ws, googleaccount.ProductCalendar)
	if err != nil || access.Token != "access-ops@example.com" {
		t.Fatalf("Access = %+v, %v", access, err)
	}
	if len(w.hooks.connected) != 1 || !slices.Contains(w.alerts.resolved, googleaccount.ProductCalendar) {
		t.Fatalf("hooks %v, resolved %v", w.hooks.connected, w.alerts.resolved)
	}
	if _, err := w.tokens.Access(context.Background(), ws, googleaccount.ProductSheets); !errors.Is(err, googleaccount.ErrNotConnected) {
		t.Fatalf("sheets must stay disconnected, got %v", err)
	}
}

func TestConnectRefusesAConsentWithoutTheProductScope(t *testing.T) {
	w := newWorld()
	w.grantFor("ops@example.com", "openid", "email")
	_, err := w.connect(t, ws, googleaccount.ProductSheets)
	if !errors.Is(err, googleaccount.ErrScopeNotGranted) {
		t.Fatalf("err = %v, want ErrScopeNotGranted", err)
	}
	if len(w.repo.accounts) != 0 || len(w.repo.bindings) != 0 {
		t.Fatal("a refused consent was stored")
	}
}

func TestConnectRefusesAReplayedState(t *testing.T) {
	w := newWorld()
	w.grantFor("ops@example.com", googleaccount.ScopeDriveFile)
	target, _ := w.connector.Start(context.Background(), StartInput{WorkspaceID: ws, Product: googleaccount.ProductSheets})
	parsed, _ := url.Parse(target)
	state := parsed.Query().Get("state")
	if _, err := w.connector.Complete(context.Background(), CompleteInput{Code: "code", State: state}); err != nil {
		t.Fatalf("first Complete: %v", err)
	}
	if _, err := w.connector.Complete(context.Background(), CompleteInput{Code: "code", State: state}); err == nil {
		t.Fatal("the same state was accepted twice")
	}
}

func TestConnectRefusesADeclinedAuthorization(t *testing.T) {
	w := newWorld()
	target, _ := w.connector.Start(context.Background(), StartInput{WorkspaceID: ws, Product: googleaccount.ProductSheets})
	parsed, _ := url.Parse(target)
	_, err := w.connector.Complete(context.Background(), CompleteInput{State: parsed.Query().Get("state"), Error: "access_denied"})
	if !errors.Is(err, googleaccount.ErrAuthorizationError) {
		t.Fatalf("err = %v", err)
	}
}

func TestIncrementalConsentKeepsTheStoredRefreshTokenAndMergesScopes(t *testing.T) {
	w := newWorld()
	w.grantFor("ops@example.com", googleaccount.ProductCalendar.Scopes()...)
	if _, err := w.connect(t, ws, googleaccount.ProductCalendar); err != nil {
		t.Fatal(err)
	}
	w.oauth.grant = googleaccount.Grant{AccessToken: "access-2", ExpiresIn: time.Hour, Scopes: []string{googleaccount.ScopeDriveFile}}
	out, err := w.connect(t, ws, googleaccount.ProductSheets)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	stored, _ := w.repo.FindByID(context.Background(), out.Account.ID)
	if stored.RefreshToken != "refresh-ops@example.com" {
		t.Fatalf("refresh token lost: %q", stored.RefreshToken)
	}
	if !googleaccount.GrantCovers(stored.Scopes, googleaccount.ProductCalendar) || !googleaccount.GrantCovers(stored.Scopes, googleaccount.ProductSheets) {
		t.Fatalf("scopes not merged: %v", stored.Scopes)
	}
	if len(w.repo.accounts) != 1 {
		t.Fatalf("the same Google account became %d rows", len(w.repo.accounts))
	}
}

func TestDisconnectingOneProductKeepsTheSharedGrant(t *testing.T) {
	w := newWorld()
	w.grantFor("ops@example.com", append(googleaccount.ProductCalendar.Scopes(), googleaccount.ScopeDriveFile)...)
	if _, err := w.connect(t, ws, googleaccount.ProductCalendar); err != nil {
		t.Fatal(err)
	}
	if _, err := w.connect(t, ws, googleaccount.ProductSheets); err != nil {
		t.Fatal(err)
	}
	if err := w.off.Disconnect(context.Background(), ws, googleaccount.ProductSheets); err != nil {
		t.Fatal(err)
	}
	if len(w.oauth.revoked) != 0 {
		t.Fatal("disconnecting Sheets revoked the grant Calendar still uses")
	}
	if _, err := w.tokens.Access(context.Background(), ws, googleaccount.ProductCalendar); err != nil {
		t.Fatalf("calendar lost access: %v", err)
	}
}

func TestDisconnectingTheLastProductRevokesAndWipes(t *testing.T) {
	w := newWorld()
	w.grantFor("ops@example.com", googleaccount.ProductCalendar.Scopes()...)
	out, err := w.connect(t, ws, googleaccount.ProductCalendar)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.off.Disconnect(context.Background(), ws, googleaccount.ProductCalendar); err != nil {
		t.Fatal(err)
	}
	if len(w.oauth.revoked) != 1 || len(w.hooks.disconnected) != 1 {
		t.Fatalf("revoked %v, hooks %v", w.oauth.revoked, w.hooks.disconnected)
	}
	stored, _ := w.repo.FindByID(context.Background(), out.Account.ID)
	if stored.Status != googleaccount.StatusDisconnected || stored.RefreshToken != "" {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestDisconnectDoesNotRevokeAGrantAnotherWorkspaceUses(t *testing.T) {
	w := newWorld()
	w.grantFor("shared@example.com", googleaccount.ScopeDriveFile)
	if _, err := w.connect(t, ws, googleaccount.ProductSheets); err != nil {
		t.Fatal(err)
	}
	if _, err := w.connect(t, "other-workspace", googleaccount.ProductSheets); err != nil {
		t.Fatal(err)
	}
	if err := w.off.Disconnect(context.Background(), ws, googleaccount.ProductSheets); err != nil {
		t.Fatal(err)
	}
	if len(w.oauth.revoked) != 0 {
		t.Fatal("revoking would cut the other workspace off the same Google account")
	}
	if _, err := w.tokens.Access(context.Background(), "other-workspace", googleaccount.ProductSheets); err != nil {
		t.Fatalf("other workspace lost access: %v", err)
	}
}

func TestSwitchingAccountsReleasesThePreviousOne(t *testing.T) {
	w := newWorld()
	w.grantFor("old@example.com", googleaccount.ScopeDriveFile)
	first, err := w.connect(t, ws, googleaccount.ProductSheets)
	if err != nil {
		t.Fatal(err)
	}
	w.grantFor("new@example.com", googleaccount.ScopeDriveFile)
	if _, err := w.connect(t, ws, googleaccount.ProductSheets); err != nil {
		t.Fatal(err)
	}
	old, _ := w.repo.FindByID(context.Background(), first.Account.ID)
	if old.Status != googleaccount.StatusDisconnected || len(w.oauth.revoked) != 1 {
		t.Fatalf("previous account kept: %+v, revoked %v", old, w.oauth.revoked)
	}
}

func TestAccessRefreshesAnExpiringToken(t *testing.T) {
	w := newWorld()
	w.grantFor("ops@example.com", googleaccount.ScopeDriveFile)
	out, err := w.connect(t, ws, googleaccount.ProductSheets)
	if err != nil {
		t.Fatal(err)
	}
	w.repo.accounts[out.Account.ID].AccessExpiresAt = time.Now().UTC().Add(time.Minute)
	w.oauth.refresh = googleaccount.Grant{AccessToken: "fresh", ExpiresIn: time.Hour}
	access, err := w.tokens.Access(context.Background(), ws, googleaccount.ProductSheets)
	if err != nil || access.Token != "fresh" {
		t.Fatalf("Access = %+v, %v", access, err)
	}
	stored, _ := w.repo.FindByID(context.Background(), out.Account.ID)
	if stored.RefreshToken != "refresh-ops@example.com" {
		t.Fatalf("a refresh without a new refresh token dropped the stored one: %q", stored.RefreshToken)
	}
}

func TestAccessMarksALostGrantAndAlerts(t *testing.T) {
	w := newWorld()
	w.grantFor("ops@example.com", googleaccount.ScopeDriveFile)
	out, err := w.connect(t, ws, googleaccount.ProductSheets)
	if err != nil {
		t.Fatal(err)
	}
	w.repo.accounts[out.Account.ID].AccessExpiresAt = time.Now().UTC().Add(-time.Minute)
	w.oauth.refreshErr = errAuth
	_, err = w.tokens.Access(context.Background(), ws, googleaccount.ProductSheets)
	if !errors.Is(err, googleaccount.ErrNeedsReconnect) {
		t.Fatalf("Access err = %v, want ErrNeedsReconnect", err)
	}
	if len(w.alerts.needsReconnect) != 1 || !slices.Contains(w.alerts.needsReconnect[0], googleaccount.ProductSheets) {
		t.Fatalf("alerts = %v", w.alerts.needsReconnect)
	}
}

func TestAccessKeepsThePairOnATransientRefreshFailure(t *testing.T) {
	w := newWorld()
	w.grantFor("ops@example.com", googleaccount.ScopeDriveFile)
	out, err := w.connect(t, ws, googleaccount.ProductSheets)
	if err != nil {
		t.Fatal(err)
	}
	w.repo.accounts[out.Account.ID].AccessExpiresAt = time.Now().UTC().Add(-time.Minute)
	w.oauth.refreshErr = errors.New("503")
	if _, err := w.tokens.Access(context.Background(), ws, googleaccount.ProductSheets); err == nil {
		t.Fatal("an expired token was handed out")
	}
	stored, _ := w.repo.FindByID(context.Background(), out.Account.ID)
	if stored.Status != googleaccount.StatusConnected || stored.RefreshToken == "" {
		t.Fatalf("a transient failure dropped the account: %+v", stored)
	}
}

func TestStatusListsEveryProduct(t *testing.T) {
	w := newWorld()
	w.grantFor("ops@example.com", googleaccount.ScopeDriveFile)
	if _, err := w.connect(t, ws, googleaccount.ProductSheets); err != nil {
		t.Fatal(err)
	}
	statuses, err := NewStatusReader(w.repo).Status(context.Background(), ws)
	if err != nil || len(statuses) != len(googleaccount.Products()) {
		t.Fatalf("Status = %+v, %v", statuses, err)
	}
	for _, s := range statuses {
		want := s.Product == googleaccount.ProductSheets
		if s.Connected != want {
			t.Fatalf("%s connected = %v, want %v", s.Product, s.Connected, want)
		}
	}
}
