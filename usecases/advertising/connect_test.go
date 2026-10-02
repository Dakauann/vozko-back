package advertising

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	ads "vozko/domain/advertising"
	fbdomain "vozko/domain/facebook"
	"vozko/usecases/shared/oauthstate"
)

type memoryKV struct{ values map[string]bool }

func (m *memoryKV) SetNX(key, _ string, _ time.Duration) (bool, error) {
	if m.values[key] {
		return false, nil
	}
	m.values[key] = true
	return true, nil
}
func (m *memoryKV) Exists(key string) (bool, error) { return m.values[key], nil }
func (m *memoryKV) Del(keys ...string) error {
	for _, k := range keys {
		delete(m.values, k)
	}
	return nil
}

type fakeOAuth struct {
	debug        *fbdomain.TokenDebug
	identifiedAs fbdomain.TokenKind
}

func (f *fakeOAuth) BuildAuthorizeURL(state string) string {
	return "https://www.facebook.com/dialog/oauth?state=" + url.QueryEscape(state)
}
func (f *fakeOAuth) ExchangeCode(context.Context, string) (*fbdomain.TokenGrant, error) {
	return &fbdomain.TokenGrant{AccessToken: "tok"}, nil
}
func (f *fakeOAuth) DebugToken(context.Context, string) (*fbdomain.TokenDebug, error) {
	return f.debug, nil
}
func (f *fakeOAuth) Identify(_ context.Context, _ string, kind fbdomain.TokenKind) (*fbdomain.GrantIdentity, error) {
	f.identifiedAs = kind
	identity := &fbdomain.GrantIdentity{AppScopedUserID: "asu-1"}
	if kind == fbdomain.TokenSystemUser {
		identity.ClientBusinessID = "biz-1"
	}
	return identity, nil
}

func connectFlow(t *testing.T, w *world, debug *fbdomain.TokenDebug) (*CompleteConnectOutput, error) {
	t.Helper()
	nonces, err := oauthstate.NewNonceStore(&memoryKV{values: map[string]bool{}}, "ads:oauth")
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := oauthstate.NewIssuer("secret", nonces, "/dashboard/advertising")
	if err != nil {
		t.Fatal(err)
	}
	uc := NewConnectUseCase(&fakeOAuth{debug: debug}, w.gateway, w.grants, w.accounts, issuer)
	authorize, err := uc.Start(StartConnectInput{WorkspaceID: "ws-9", UserID: "u-1", Popup: true})
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(authorize)
	return uc.Complete(context.Background(), CompleteConnectInput{Code: "code", State: parsed.Query().Get("state")})
}

func fullDebug() *fbdomain.TokenDebug {
	return &fbdomain.TokenDebug{Valid: true, Type: "SYSTEM_USER", Scopes: ads.RequiredScopes(), GranularScopes: map[string][]string{}}
}

func TestConnectStoresOnlyAccountsTheCustomerPicked(t *testing.T) {
	w := newWorld()
	w.accounts.byID = map[string]*ads.AdAccount{}
	w.gateway.accounts = append(w.gateway.accounts, ads.RemoteAdAccount{MetaAccountID: "222", Name: "Outra", Currency: "BRL", Timezone: "America/Sao_Paulo"})
	debug := fullDebug()
	debug.GranularScopes[ads.ScopeAdsManagement] = []string{"222"}
	out, err := connectFlow(t, w, debug)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Accounts) != 1 || out.Accounts[0].MetaAccountID != "222" || out.ConnectedCount() != 1 {
		t.Fatalf("accounts %+v", out.Accounts)
	}
	if !out.Popup {
		t.Fatal("popup flag lost")
	}
}

func TestConnectWithoutTheAdsPermissionsIsRefusedAndStoresNothing(t *testing.T) {
	w := newWorld()
	w.grants.byID = map[string]*ads.Grant{}
	debug := fullDebug()
	debug.Scopes = []string{ads.ScopeAdsRead}
	_, err := connectFlow(t, w, debug)
	if !errors.Is(err, ads.ErrMissingScopes) || len(w.grants.byID) != 0 {
		t.Fatalf("err %v grants %d", err, len(w.grants.byID))
	}
	var connectErr *ConnectError
	if !errors.As(err, &connectErr) || !connectErr.Popup {
		t.Fatalf("popup context lost: %v", err)
	}
}

func TestAccountOwnedByAnotherWorkspaceIsReportedNotStolen(t *testing.T) {
	w := newWorld()
	out, err := connectFlow(t, w, fullDebug())
	if err != nil {
		t.Fatal(err)
	}
	if out.Accounts[0].Outcome != OutcomeLinkedElsewhere || w.accounts.byID["acc-1"].WorkspaceID != "ws-1" {
		t.Fatalf("outcome %+v", out.Accounts)
	}
}

func TestDeclinedAuthorizationIsAnError(t *testing.T) {
	w := newWorld()
	nonces, _ := oauthstate.NewNonceStore(&memoryKV{values: map[string]bool{}}, "ads:oauth")
	issuer, _ := oauthstate.NewIssuer("secret", nonces, "/dashboard/advertising")
	uc := NewConnectUseCase(&fakeOAuth{debug: fullDebug()}, w.gateway, w.grants, w.accounts, issuer)
	authorize, _ := uc.Start(StartConnectInput{WorkspaceID: "ws-9"})
	parsed, _ := url.Parse(authorize)
	_, err := uc.Complete(context.Background(), CompleteConnectInput{State: parsed.Query().Get("state"), Error: "access_denied"})
	if !errors.Is(err, ads.ErrAuthorizationDenied) {
		t.Fatalf("err %v", err)
	}
}

func TestConnectWithAPersonalTokenStoresAUserGrantWithoutBusiness(t *testing.T) {
	w := newWorld()
	w.grants.byID = map[string]*ads.Grant{}
	debug := fullDebug()
	debug.Type = "USER"
	if _, err := connectFlow(t, w, debug); err != nil {
		t.Fatal(err)
	}
	for _, g := range w.grants.byID {
		if g.TokenKind != fbdomain.TokenUser || g.ClientBusinessID != "" {
			t.Fatalf("grant %+v", g)
		}
	}
	if len(w.grants.byID) != 1 {
		t.Fatalf("grants %d", len(w.grants.byID))
	}
}
