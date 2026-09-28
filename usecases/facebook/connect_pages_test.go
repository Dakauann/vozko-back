package facebook

import (
	"context"
	"errors"
	"testing"
	"time"

	fbdomain "vozko/domain/facebook"
	"vozko/usecases/shared/oauthstate"
)

type memoryKV struct{ values map[string]string }

func (m *memoryKV) SetNX(key, value string, _ time.Duration) (bool, error) {
	if _, ok := m.values[key]; ok {
		return false, nil
	}
	m.values[key] = value
	return true, nil
}
func (m *memoryKV) Exists(key string) (bool, error) { _, ok := m.values[key]; return ok, nil }
func (m *memoryKV) Del(keys ...string) error {
	for _, k := range keys {
		delete(m.values, k)
	}
	return nil
}

func newIssuer(t *testing.T) *oauthstate.Issuer {
	t.Helper()
	nonces, err := oauthstate.NewNonceStore(&memoryKV{values: map[string]string{}}, "fb:oauth")
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := oauthstate.NewIssuer("secret", nonces, "/dashboard/facebook-pages")
	if err != nil {
		t.Fatal(err)
	}
	return issuer
}

func fullGrantDebug(pageIDs ...string) *fbdomain.TokenDebug {
	granular := map[string][]string{}
	for _, scope := range fbdomain.RequiredScopes() {
		granular[scope] = append([]string(nil), pageIDs...)
	}
	granular[fbdomain.ScopeBusinessManagement] = nil
	return &fbdomain.TokenDebug{Valid: true, Type: "SYSTEM_USER", AppScopedUser: "asid-1", Scopes: fbdomain.RequiredScopes(), GranularScopes: granular}
}

type connectFixture struct {
	uc       *ConnectPagesUseCase
	oauth    *fakeOAuth
	sub      *fakeSubscription
	grants   *fakeGrants
	pages    *fakePages
	pictures *fakePictures
	issuer   *oauthstate.Issuer
}

func newConnectFixture(t *testing.T, existing ...*fbdomain.Page) *connectFixture {
	t.Helper()
	f := &connectFixture{
		oauth: &fakeOAuth{
			grant:    &fbdomain.TokenGrant{AccessToken: "bisu"},
			debug:    fullGrantDebug("111", "222"),
			identity: &fbdomain.GrantIdentity{AppScopedUserID: "asid-1", ClientBusinessID: "biz-1"},
			pages: []*fbdomain.RemotePage{
				{FBPageID: "111", Name: "Loja", AccessToken: "pt-111", Tasks: []fbdomain.Task{fbdomain.TaskManage}, PictureURL: "https://cdn/111.jpg"},
				{FBPageID: "222", Name: "Outra", AccessToken: "pt-222", Tasks: []fbdomain.Task{fbdomain.TaskAnalyze}},
			},
		},
		sub:      &fakeSubscription{},
		grants:   newFakeGrants(),
		pages:    newFakePages(existing...),
		pictures: &fakePictures{},
		issuer:   newIssuer(t),
	}
	f.uc = NewConnectPagesUseCase(f.oauth, f.sub, f.grants, f.pages, f.pictures, f.issuer)
	return f
}

func (f *connectFixture) state(t *testing.T) string {
	t.Helper()
	out, err := f.uc.Start(context.Background(), StartConnectInput{WorkspaceID: "ws-1", UserID: "u-1", Popup: true})
	if err != nil {
		t.Fatal(err)
	}
	return out.State
}

func outcomeFor(out *CompleteConnectOutput, fbPageID string) PageOutcome {
	for _, p := range out.Pages {
		if p.FBPageID == fbPageID {
			return p
		}
	}
	return PageOutcome{}
}

func TestConnectCreatesEveryGrantedPage(t *testing.T) {
	f := newConnectFixture(t)
	out, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: f.state(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Pages) != 2 || !out.Popup {
		t.Fatalf("output = %+v", out)
	}
	loja := outcomeFor(out, "111")
	if loja.Outcome != OutcomeConnected || len(loja.Missing) != 0 {
		t.Fatalf("admin page outcome = %+v", loja)
	}
	outra := outcomeFor(out, "222")
	if outra.Outcome != OutcomeConnected || len(outra.Missing) == 0 {
		t.Fatalf("analyze-only page must report its missing capabilities: %+v", outra)
	}
	stored, _ := f.pages.FindByFBPageID(context.Background(), "111")
	if stored.WorkspaceID != "ws-1" || stored.PageToken != "pt-111" || stored.Status != fbdomain.StatusConnected || stored.GrantID == "" {
		t.Fatalf("stored page = %+v", stored)
	}
	if stored.WebhookSubscribedAt == nil || stored.PictureStorageKey == "" {
		t.Fatalf("subscription or picture not recorded: %+v", stored)
	}
	if len(f.sub.subscribed) != 1 {
		t.Fatalf("only pages that can subscribe are subscribed, got %v", f.sub.subscribed)
	}
}

func TestConnectNeverMovesAPageFromAnotherWorkspace(t *testing.T) {
	f := newConnectFixture(t, &fbdomain.Page{ID: "page-111", WorkspaceID: "ws-other", FBPageID: "111", Status: fbdomain.StatusConnected})
	out, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: f.state(t)})
	if err != nil {
		t.Fatal(err)
	}
	if outcomeFor(out, "111").Outcome != OutcomeLinkedElsewhere {
		t.Fatalf("outcome = %+v", outcomeFor(out, "111"))
	}
	stored, _ := f.pages.FindByFBPageID(context.Background(), "111")
	if stored.WorkspaceID != "ws-other" {
		t.Fatal("a page was moved across workspaces")
	}
}

func TestReconnectRestoresAndKeepsTheConfig(t *testing.T) {
	agent := "agent-1"
	existing := &fbdomain.Page{ID: "page-111", WorkspaceID: "ws-1", FBPageID: "111", Status: fbdomain.StatusTokenRevoked, AgentID: &agent, EnableAgentResponses: true}
	f := newConnectFixture(t, existing)
	f.pages.deleted["page-111"] = true

	out, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: f.state(t)})
	if err != nil {
		t.Fatal(err)
	}
	if outcomeFor(out, "111").Outcome != OutcomeReconnected {
		t.Fatalf("outcome = %+v", outcomeFor(out, "111"))
	}
	stored, _ := f.pages.FindByFBPageID(context.Background(), "111")
	if stored == nil || stored.Status != fbdomain.StatusConnected || stored.AgentID == nil || !stored.EnableAgentResponses || stored.PageToken != "pt-111" {
		t.Fatalf("stored = %+v", stored)
	}
	if len(f.pages.restored) != 1 {
		t.Fatal("soft deleted page not restored")
	}
}

func TestConnectRefusesAGrantWithoutThePageListPermission(t *testing.T) {
	f := newConnectFixture(t)
	f.oauth.debug = &fbdomain.TokenDebug{Valid: true, Scopes: []string{fbdomain.ScopeMessaging}}
	if _, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: f.state(t)}); !errors.Is(err, fbdomain.ErrGrantUnverifiable) {
		t.Fatalf("got %v", err)
	}
}

func TestAnUnrestrictedSystemUserGrantConnectsTheListedPages(t *testing.T) {
	f := newConnectFixture(t)
	f.oauth.debug = fullGrantDebug()
	out, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: f.state(t)})
	if err != nil || len(out.Pages) != 2 || outcomeFor(out, "111").Outcome != OutcomeConnected {
		t.Fatalf("out %+v %v", out, err)
	}
	stored, _ := f.pages.FindByFBPageID(context.Background(), "111")
	if len(stored.GrantedScopes) == 0 || !stored.Can(fbdomain.CapMessaging) {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestConnectRefusesAnInvalidToken(t *testing.T) {
	f := newConnectFixture(t)
	f.oauth.debug = &fbdomain.TokenDebug{Valid: false}
	if _, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: f.state(t)}); err == nil {
		t.Fatal("invalid token accepted")
	}
}

func TestConnectWithNoGrantedPagesFails(t *testing.T) {
	f := newConnectFixture(t)
	f.oauth.debug = fullGrantDebug("999")
	if _, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: f.state(t)}); !errors.Is(err, fbdomain.ErrNoPagesGranted) {
		t.Fatalf("got %v", err)
	}
}

func TestDeclinedAuthorizationIsReported(t *testing.T) {
	f := newConnectFixture(t)
	_, err := f.uc.Complete(context.Background(), CompleteConnectInput{State: f.state(t), Error: "access_denied", ErrorReason: "user_denied"})
	if !errors.Is(err, fbdomain.ErrAuthorizationDenied) {
		t.Fatalf("got %v", err)
	}
}

func TestReplayedStateIsRejected(t *testing.T) {
	f := newConnectFixture(t)
	state := f.state(t)
	if _, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: state}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: state}); !errors.Is(err, oauthstate.ErrReplayedState) {
		t.Fatalf("got %v", err)
	}
}

func TestSubscriptionFailureIsRecordedNotSwallowed(t *testing.T) {
	f := newConnectFixture(t)
	f.sub.err = errors.New("meta down")
	out, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: f.state(t)})
	if err != nil {
		t.Fatal(err)
	}
	if outcomeFor(out, "111").Warning == "" {
		t.Fatal("subscription failure must surface as a warning")
	}
	stored, _ := f.pages.FindByFBPageID(context.Background(), "111")
	if stored.StatusReason != ReasonWebhookSubscriptionFailed {
		t.Fatalf("status reason = %q", stored.StatusReason)
	}
}

func TestGrantIsStoredWithItsKind(t *testing.T) {
	f := newConnectFixture(t)
	if _, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: f.state(t)}); err != nil {
		t.Fatal(err)
	}
	g := f.grants.byID["grant-asid-1"]
	if g == nil || g.TokenKind != fbdomain.TokenSystemUser || g.ClientBusinessID != "biz-1" || g.WorkspaceID != "ws-1" || g.ConnectedBy != "u-1" {
		t.Fatalf("grant = %+v", g)
	}
}

func TestFailuresAfterTheStateCarryThePopupFlag(t *testing.T) {
	f := newConnectFixture(t)
	f.oauth.debug = &fbdomain.TokenDebug{Valid: false}
	_, err := f.uc.Complete(context.Background(), CompleteConnectInput{Code: "c", State: f.state(t)})
	var ce *ConnectError
	if !errors.As(err, &ce) || !ce.Popup || ce.ReturnPath != "/dashboard/facebook-pages" {
		t.Fatalf("got %#v", err)
	}
}
