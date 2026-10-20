package mercadolivre

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	mldomain "vozko/domain/mercadolivre"
	"vozko/usecases/shared/oauthstate"
)

type memoryKV struct {
	mu     sync.Mutex
	values map[string]string
}

func (m *memoryKV) SetNX(key, value string, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.values[key]; ok {
		return false, nil
	}
	m.values[key] = value
	return true, nil
}

func (m *memoryKV) GetString(key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values[key], nil
}

func (m *memoryKV) Exists(key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.values[key]
	return ok, nil
}

func (m *memoryKV) Del(keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.values, k)
	}
	return nil
}

type fakePKCE struct{}

func (fakePKCE) NewVerifier() (string, error) {
	return "verifier-0123456789-0123456789-0123456789-abc", nil
}

func (fakePKCE) Challenge(v string) (string, error) { return "challenge-of-" + v[:8], nil }

type connectFixture struct {
	uc        *ConnectUseCase
	accounts  *fakeAccounts
	oauth     *fakeOAuth
	users     *fakeUsers
	questions *fakeQuestions
	messages  *fakeMessages
	alerts    *fakeAlerts
}

func newConnectFixture(t *testing.T) connectFixture {
	t.Helper()
	kv := &memoryKV{values: map[string]string{}}
	nonces, err := oauthstate.NewNonceStore(kv, "ml:oauth")
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := oauthstate.NewIssuer("test-secret", nonces, AccountsPath)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := oauthstate.NewSealedIssuer(issuer, kv, "ml:oauth")
	if err != nil {
		t.Fatal(err)
	}
	f := connectFixture{
		accounts: newFakeAccounts(),
		oauth: &fakeOAuth{exchange: &mldomain.TokenGrant{
			AccessToken: "APP_USR-1", RefreshToken: "TG-1", ExpiresIn: 6 * time.Hour, UserID: "415458330", Scopes: mldomain.RequiredScopes(),
		}},
		users:     &fakeUsers{me: &mldomain.User{ID: "415458330", Nickname: "LOJA", SiteID: mldomain.SiteMLB}},
		questions: &fakeQuestions{},
		messages:  &fakeMessages{},
		alerts:    &fakeAlerts{},
	}
	f.uc = NewConnectUseCase(ConnectDeps{
		Accounts: f.accounts,
		Services: Services{OAuth: f.oauth, Users: f.users, Questions: f.questions, Messages: f.messages, Errors: fakeClassifier{}},
		States:   sealed,
		PKCE:     fakePKCE{},
		Alerts:   f.alerts,
		Metrics:  newFakeMetrics(),
	})
	f.uc.now = fixedClock
	return f
}

func (f connectFixture) start(t *testing.T, workspaceID string) string {
	t.Helper()
	out, err := f.uc.Start(context.Background(), StartConnectInput{WorkspaceID: workspaceID, UserID: "u-1", Popup: true, ReturnPath: "/dashboard/mercadolivre-accounts"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(out.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.AuthorizeURL, "verifier-0123") {
		t.Fatal("the PKCE verifier must never leave the server")
	}
	if !strings.Contains(out.AuthorizeURL, "code_challenge=challenge-of-verifier") {
		t.Fatalf("authorize url = %s", out.AuthorizeURL)
	}
	return u.Query().Get("state")
}

func TestConnectCreatesTheAccountWithProbedCapabilities(t *testing.T) {
	f := newConnectFixture(t)
	state := f.start(t, "ws-1")
	out, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "TG-code", State: state})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !out.Popup || out.Reconnected || out.Account == nil {
		t.Fatalf("out = %+v", out)
	}
	a := out.Account
	if a.Status != mldomain.StatusConnected || a.Nickname != "LOJA" || !a.CanSend() || !a.CanReadQuestions || a.WorkspaceID != "ws-1" {
		t.Fatalf("account = %+v", a)
	}
	if !a.AccessExpiresAt.Equal(testNow.Add(6*time.Hour)) || a.RefreshToken != "TG-1" {
		t.Fatalf("tokens = %+v", a)
	}
	if f.oauth.verifier != "verifier-0123456789-0123456789-0123456789-abc" {
		t.Fatalf("the exchange must carry the sealed verifier, got %q", f.oauth.verifier)
	}
	if len(f.alerts.reconnected) != 1 {
		t.Fatal("a connect resolves any pending reconnect alert")
	}
	if _, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "TG-code", State: state}); !errors.Is(err, oauthstate.ErrReplayedState) {
		t.Fatalf("replayed state = %v", err)
	}
}

func TestConnectRefusals(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f connectFixture)
		input func(state string) CompleteConnectInput
		want  error
	}{
		{"operator in the callback", nil, func(s string) CompleteConnectInput {
			return CompleteConnectInput{State: s, Error: "invalid_operator_user_id", ErrorDescription: "operator"}
		}, mldomain.ErrNotAccountAdmin},
		{"operator at the exchange", func(f connectFixture) { f.oauth.exchangeErr = &mlErr{msg: "invalid_operator_user_id", operator: true} },
			nil, mldomain.ErrNotAccountAdmin},
		{"declined", nil, func(s string) CompleteConnectInput { return CompleteConnectInput{State: s, Error: "access_denied"} }, ErrAuthorizationDeclined},
		{"no code", nil, func(s string) CompleteConnectInput { return CompleteConnectInput{State: s} }, ErrAuthorizationCode},
		{"another site", func(f connectFixture) { f.users.me.SiteID = "MLA" }, nil, mldomain.ErrSiteNotSupported},
		{"token for someone else", func(f connectFixture) { f.users.me.ID = "1" }, nil, ErrIdentityMismatch},
		{"read only grant", func(f connectFixture) { f.oauth.exchange.Scopes = []string{"offline_access", "read"} }, nil, mldomain.ErrPermissionMissing},
		{"messages permission missing", func(f connectFixture) {
			f.messages.unreadErr = &mlErr{msg: "PA_UNAUTHORIZED_RESULT_FROM_POLICIES", perm: true}
		}, nil, mldomain.ErrPermissionMissing},
		{"questions permission missing", func(f connectFixture) {
			f.questions.searchErr = &mlErr{msg: "PA_UNAUTHORIZED_RESULT_FROM_POLICIES", perm: true}
		}, nil, mldomain.ErrPermissionMissing},
		{"probe unreachable", func(f connectFixture) { f.messages.unreadErr = &mlErr{msg: "timeout", retryable: true} }, nil, ErrProbeInconclusive},
		{"linked elsewhere", func(f connectFixture) {
			other := connectedAccount()
			other.WorkspaceID = "ws-2"
			f.accounts.put(other)
		}, nil, mldomain.ErrAccountAlreadyLinked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newConnectFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			state := f.start(t, "ws-1")
			in := CompleteConnectInput{Code: "TG-code", State: state}
			if tc.input != nil {
				in = tc.input(state)
			}
			out, err := f.uc.Complete(context.Background(), in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if out == nil || !out.Popup || out.ReturnPath == "" {
				t.Fatalf("a refusal after the state was read must still answer the popup: %+v", out)
			}
			if tc.name != "linked elsewhere" {
				if live, err := f.accounts.FindLiveByMLUserID(context.Background(), "415458330"); err == nil {
					t.Fatalf("a refused connect stored an account: %+v", live)
				}
			}
		})
	}
}

func TestReconnectRestoresTheDisconnectedRowAndKeepsItsSettings(t *testing.T) {
	f := newConnectFixture(t)
	old := connectedAccount()
	old.AgentID = strPtr("agent-1")
	old.EnableAgentResponses = true
	f.accounts.put(old)
	if err := f.accounts.Disconnect(context.Background(), old.ID, mldomain.StatusDisconnected, "bye", testNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	state := f.start(t, "ws-1")
	out, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "TG-code", State: state})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Reconnected || out.Account.ID != old.ID {
		t.Fatalf("out = %+v", out)
	}
	stored := f.accounts.get(old.ID)
	if stored.Status != mldomain.StatusConnected || stored.PurgeAfter != nil || stored.AgentID == nil || !stored.EnableAgentResponses || stored.AccessToken != "APP_USR-1" {
		t.Fatalf("restored = %+v", stored)
	}
	if live, err := f.accounts.FindLiveByMLUserID(context.Background(), old.MLUserID); err != nil || live.ID != old.ID {
		t.Fatalf("live = %+v %v", live, err)
	}
}

func TestReconnectOfALiveAccountRotatesItsTokens(t *testing.T) {
	f := newConnectFixture(t)
	live := connectedAccount()
	live.Status = mldomain.StatusNeedsReconnect
	live.AccessToken, live.RefreshToken = "", ""
	f.accounts.put(live)
	state := f.start(t, "ws-1")
	out, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "TG-code", State: state})
	if err != nil || !out.Reconnected {
		t.Fatalf("out = %+v err = %v", out, err)
	}
	if got := f.accounts.get(live.ID); got.Status != mldomain.StatusConnected || got.RefreshToken != "TG-1" {
		t.Fatalf("account = %+v", got)
	}
}

func TestStartNeedsAWorkspace(t *testing.T) {
	f := newConnectFixture(t)
	if _, err := f.uc.Start(context.Background(), StartConnectInput{}); !errors.Is(err, mldomain.ErrWorkspaceIDRequired) {
		t.Fatalf("err = %v", err)
	}
}
