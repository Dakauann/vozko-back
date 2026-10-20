package tiktok

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"testing"
	"time"

	ttdomain "vozko/domain/tiktok"
	"vozko/usecases/shared/oauthstate"
)

type memoryNonces struct {
	mu   sync.Mutex
	live map[string]string
}

func (m *memoryNonces) Issue(nonce, workspaceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.live == nil {
		m.live = map[string]string{}
	}
	m.live[nonce] = workspaceID
	return nil
}

func (m *memoryNonces) Consume(nonce string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.live[nonce]; !ok {
		return errors.New("nonce already used or unknown")
	}
	delete(m.live, nonce)
	return nil
}

type connectHarness struct {
	uc       *ConnectUseCase
	accounts *fakeAccounts
	svc      *fakeServices
	alerts   *fakeAlerts
	objects  *fakeObjects
}

func newConnectHarness(t *testing.T) connectHarness {
	t.Helper()
	issuer, err := oauthstate.NewIssuer("state-secret", &memoryNonces{}, "/dashboard/tiktok-accounts")
	if err != nil {
		t.Fatal(err)
	}
	accounts := newFakeAccounts()
	svc := newFakeServices()
	alerts := &fakeAlerts{}
	objects := &fakeObjects{}
	metrics := &fakeMetrics{}
	svc.grant = ttdomain.Grant{OpenID: "_000abc", AccessToken: "act.1", RefreshToken: "rft.1", ExpiresIn: 24 * time.Hour, RefreshExpiresIn: 365 * 24 * time.Hour}
	svc.info = ttdomain.TokenInfo{OpenID: "_000abc", Scopes: []string{ttdomain.ScopeUserInfoBasic, ttdomain.ScopeVideoList, ttdomain.ScopeCommentList, ttdomain.ScopeCommentManage, ttdomain.ScopeMessageRead, ttdomain.ScopeMessageSend}}
	svc.profile = ttdomain.BusinessProfile{Username: "loja.oficial", DisplayName: "Loja Oficial", AvatarURL: "https://p16.tiktokcdn.com/avatar.jpeg", HasAdvancedAccess: true}
	uc := NewConnectUseCase(ConnectDeps{
		Accounts: accounts,
		Services: svc.services(fakeClassifier{}),
		States:   issuer,
		Webhooks: NewWebhookSubscriptions(svc, "https://api.example.com/webhooks/tiktok", metrics),
		Images:   NewImageStore(&fakeFetcher{data: map[string][]byte{"https://p16.tiktokcdn.com/avatar.jpeg": []byte("\xff\xd8\xff\xe0jpeg")}}, objects),
		Alerts:   alerts,
		Metrics:  metrics,
	})
	uc.now = fixedClock
	return connectHarness{uc: uc, accounts: accounts, svc: svc, alerts: alerts, objects: objects}
}

func (h connectHarness) state(t *testing.T, workspaceID string) string {
	t.Helper()
	out, err := h.uc.Start(context.Background(), StartConnectInput{WorkspaceID: workspaceID, UserID: "user-1", ReturnPath: "/dashboard/tiktok-accounts", Popup: true})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(out.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("state")
}

func TestConnectCreatesAnAccountWithCapabilitiesFromTheGrantedScopes(t *testing.T) {
	h := newConnectHarness(t)
	out, err := h.uc.Complete(context.Background(), CompleteConnectInput{Code: "code", State: h.state(t, "ws-1")})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	a := out.Account
	if a.WorkspaceID != "ws-1" || a.OpenID != "_000abc" || a.Status != ttdomain.StatusConnected || out.Reconnected || !out.Popup {
		t.Fatalf("account %+v out %+v", a, out)
	}
	if !a.Capabilities.CanReceiveMessages || !a.Capabilities.CanSendMessages || !a.Capabilities.CanManageComments || a.Capabilities.CanManageInboxProfile {
		t.Errorf("capabilities = %+v", a.Capabilities)
	}
	if !a.AccessExpiresAt.Equal(testNow.Add(24*time.Hour)) || !a.RefreshExpiresAt.Equal(testNow.Add(365*24*time.Hour)) {
		t.Errorf("expiries %v %v", a.AccessExpiresAt, a.RefreshExpiresAt)
	}
	if a.AvatarKey != AccountAvatarKey(a.ID) || h.objects.uploaded[AccountAvatarKey(a.ID)] == nil {
		t.Errorf("avatar not re-hosted: %q", a.AvatarKey)
	}
	if !a.WebhooksHealthy || len(h.svc.subscribed) != 2 {
		t.Errorf("webhooks healthy %v subscribed %v", a.WebhooksHealthy, h.svc.subscribed)
	}
	if len(h.alerts.healthy) != 1 {
		t.Errorf("health alerts not resolved: %+v", h.alerts)
	}
}

func TestAStateCanBeRedeemedOnlyOnce(t *testing.T) {
	h := newConnectHarness(t)
	state := h.state(t, "ws-1")
	if _, err := h.uc.Complete(context.Background(), CompleteConnectInput{Code: "code", State: state}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.uc.Complete(context.Background(), CompleteConnectInput{Code: "code", State: state}); err == nil {
		t.Fatal("a replayed state must be refused")
	}
}

func TestConnectRefusesDeclinesMissingCodesAndOtherAccountsTokens(t *testing.T) {
	h := newConnectHarness(t)
	if _, err := h.uc.Complete(context.Background(), CompleteConnectInput{State: h.state(t, "ws-1"), Error: "access_denied"}); !errors.Is(err, ErrAuthorizationDeclined) {
		t.Errorf("declined: %v", err)
	}
	if _, err := h.uc.Complete(context.Background(), CompleteConnectInput{State: h.state(t, "ws-1")}); !errors.Is(err, ErrAuthorizationCode) {
		t.Errorf("no code: %v", err)
	}
	h.svc.info.OpenID = "_000other"
	if _, err := h.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: h.state(t, "ws-1")}); !errors.Is(err, ttdomain.ErrIdentityMismatch) {
		t.Errorf("mismatch: %v", err)
	}
}

func TestConnectRefusesAGrantWithNeitherCommentsNorMessages(t *testing.T) {
	h := newConnectHarness(t)
	h.svc.info.Scopes = []string{ttdomain.ScopeUserInfoBasic, ttdomain.ScopeVideoList}
	if _, err := h.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: h.state(t, "ws-1")}); !errors.Is(err, ttdomain.ErrScopeMissing) {
		t.Fatalf("err = %v", err)
	}
	if len(h.accounts.byID) != 0 {
		t.Fatal("a refused connect must not save the account")
	}
}

func TestConnectRefusesAMessagingOnlyGrantOnAnIneligibleAccount(t *testing.T) {
	h := newConnectHarness(t)
	h.svc.info.Scopes = []string{ttdomain.ScopeUserInfoBasic, ttdomain.ScopeMessageRead, ttdomain.ScopeMessageSend}
	h.svc.profile.HasAdvancedAccess = false
	if _, err := h.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: h.state(t, "ws-1")}); !errors.Is(err, ttdomain.ErrNotEligibleAccount) {
		t.Fatalf("err = %v", err)
	}
}

func TestARegionRefusalTurnsMessagingOffButKeepsComments(t *testing.T) {
	h := newConnectHarness(t)
	h.svc.probeErr = codedError{permission: true, refusal: true}
	out, err := h.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: h.state(t, "ws-1")})
	if err != nil {
		t.Fatal(err)
	}
	if out.Account.Capabilities.CanReceiveMessages || out.Account.Capabilities.CanSendMessages || !out.Account.Capabilities.CanReadComments || out.Account.StatusReason == "" {
		t.Fatalf("account = %+v", out.Account)
	}
}

func TestAnInconclusiveProbeRefusesTheConnect(t *testing.T) {
	h := newConnectHarness(t)
	h.svc.probeErr = codedError{retry: true}
	if _, err := h.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: h.state(t, "ws-1")}); !errors.Is(err, ErrProbeInconclusive) {
		t.Fatalf("err = %v", err)
	}
}

func TestAnAccountLiveInAnotherWorkspaceIsRefused(t *testing.T) {
	h := newConnectHarness(t)
	h.accounts.put(&ttdomain.Account{WorkspaceID: "ws-other", OpenID: "_000abc", Status: ttdomain.StatusConnected})
	if _, err := h.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: h.state(t, "ws-1")}); !errors.Is(err, ttdomain.ErrAccountAlreadyLinked) {
		t.Fatalf("err = %v", err)
	}
}

func TestReconnectingInTheSameWorkspaceKeepsTheAccountAndClearsThePurge(t *testing.T) {
	h := newConnectHarness(t)
	purge := testNow.Add(10 * 24 * time.Hour)
	existing := h.accounts.put(&ttdomain.Account{WorkspaceID: "ws-1", OpenID: "_000abc", Status: ttdomain.StatusRevoked, PurgeAfter: &purge, AgentID: ptrStr("agent-1")})
	out, err := h.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: h.state(t, "ws-1")})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Reconnected || out.Account.ID != existing.ID || out.Account.PurgeAfter != nil || out.Account.Status != ttdomain.StatusConnected {
		t.Fatalf("out %+v account %+v", out, out.Account)
	}
}

func TestAnItemFilteredSubscriptionIsUnhealthyAndNeverRewritten(t *testing.T) {
	h := newConnectHarness(t)
	h.svc.subscriptions[ttdomain.WebhookComment] = &ttdomain.WebhookSubscription{Kind: ttdomain.WebhookComment, CallbackURL: "https://api.example.com/webhooks/tiktok", ItemFilter: []string{"1"}}
	out, err := h.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: h.state(t, "ws-1")})
	if err != nil {
		t.Fatal(err)
	}
	if out.Account.WebhooksHealthy || len(h.alerts.webhooks) != 1 {
		t.Fatalf("healthy %v alerts %+v", out.Account.WebhooksHealthy, h.alerts)
	}
	for _, kind := range h.svc.subscribed {
		if kind == ttdomain.WebhookComment {
			t.Fatal("a filtered subscription must never be rewritten automatically")
		}
	}
}

func ptrStr(s string) *string { return &s }
