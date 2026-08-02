package telegram

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	tgdomain "vozko/domain/telegram"
	workspace_domain "vozko/domain/workspace"
)

// recordingAC captures the RBAC resource/action each route was registered with,
// so the test asserts authorization is actually applied rather than just that a
// path exists.
type recordingAC struct {
	calls map[string]string
}

func (r *recordingAC) fn(resource workspace_domain.Resource, action workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.calls[req.Method+" "+req.URL.Path] = string(resource) + ":" + string(action)
		// The downstream handler is deliberately NOT invoked. This test asserts
		// that a path exists and which resource:action guards it; running the
		// handler body would only exercise a zero-valued usecase graph, and
		// making those tolerate nil dependencies would hide real wiring bugs
		// rather than catch them.
		if h == nil {
			t := "handler must not be nil"
			http.Error(w, t, http.StatusInternalServerError)
		}
	}
}

func TestRegisterProtectedRoutes_AppliesRBAC(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

	// A zero handler is enough: this exercises registration, not behaviour.
	RegisterProtectedRoutes(router, &Handler{}, ac.fn)

	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodPost, "/telegram/accounts", "telegram_accounts:create"},
		{http.MethodGet, "/telegram/accounts", "telegram_accounts:read"},
		{http.MethodGet, "/telegram/accounts/acct-1", "telegram_accounts:read"},
		{http.MethodPut, "/telegram/accounts/acct-1", "telegram_accounts:update"},
		{http.MethodDelete, "/telegram/accounts/acct-1", "telegram_accounts:delete"},
		// Re-registering is a repair on an existing account, not a new connection.
		{http.MethodPost, "/telegram/accounts/acct-1/webhook", "telegram_accounts:update"},
		{http.MethodGet, "/telegram/accounts/acct-1/deep-links", "telegram_accounts:read"},
		{http.MethodPost, "/telegram/accounts/acct-1/deep-links", "telegram_accounts:update"},
		{http.MethodDelete, "/telegram/accounts/acct-1/deep-links/tok-1", "telegram_accounts:update"},
	}

	for _, c := range cases {
		var match mux.RouteMatch
		req := httptest.NewRequest(c.method, c.path, nil)

		if !router.Match(req, &match) {
			t.Errorf("%s %s is not registered", c.method, c.path)
			continue
		}

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		got := ac.calls[c.method+" "+c.path]
		if got != c.want {
			t.Errorf("%s %s guarded by %q, want %q", c.method, c.path, got, c.want)
		}
	}
}

// A nil handler means the channel is not wired. Registering routes whose methods
// would nil-panic on the first request is worse than having no routes.
func TestRegisterProtectedRoutes_NilHandlerRegistersNothing(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

	RegisterProtectedRoutes(router, nil, ac.fn)

	var match mux.RouteMatch
	if router.Match(httptest.NewRequest(http.MethodGet, "/telegram/accounts", nil), &match) {
		t.Error("no route may be registered when the channel is disabled")
	}
}

// The webhook is public by necessity — Telegram calls it — and is authenticated
// by the per-account secret token instead. It must be registered on the PUBLIC
// router, and it must carry the account id in the path, because an Update object
// identifies no bot.
func TestRegisterPublicRoutes(t *testing.T) {
	router := mux.NewRouter()
	RegisterPublicRoutes(router, NewWebhookHandler(&stubAccounts{}, &stubPublisher{}))

	var match mux.RouteMatch
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram/acct-1", nil)
	if !router.Match(req, &match) {
		t.Fatal("the webhook route is not registered on the public router")
	}
	if got := match.Vars["accountId"]; got != "acct-1" {
		t.Errorf("accountId var = %q, want acct-1 — tenancy comes from the URL", got)
	}

	// Telegram has no GET handshake, unlike Meta: there is nothing to verify.
	if router.Match(httptest.NewRequest(http.MethodGet, "/webhooks/telegram/acct-1", nil), &match) &&
		match.MatchErr == nil {
		t.Error("GET must not be registered; Telegram never sends one")
	}
}

func TestRegisterPublicRoutes_NilHandlerRegistersNothing(t *testing.T) {
	router := mux.NewRouter()
	RegisterPublicRoutes(router, nil)

	var match mux.RouteMatch
	if router.Match(httptest.NewRequest(http.MethodPost, "/webhooks/telegram/acct-1", nil), &match) {
		t.Error("no webhook route may exist when the channel is disabled")
	}
}

// The registered path and the constant the connect flow hands to Telegram must be
// the same string. If they drift, setWebhook succeeds and every delivery 404s —
// a failure with no error anywhere, only silence.
func TestWebhookPathMatchesTheRegisteredURL(t *testing.T) {
	url := tgdomain.WebhookURLFor("https://api.example.com", "acct-1")
	const want = "https://api.example.com/webhooks/telegram/acct-1"
	if url != want {
		t.Fatalf("WebhookURLFor = %q, want %q", url, want)
	}

	router := mux.NewRouter()
	RegisterPublicRoutes(router, NewWebhookHandler(&stubAccounts{}, &stubPublisher{}))

	var match mux.RouteMatch
	if !router.Match(httptest.NewRequest(http.MethodPost, "/webhooks/telegram/acct-1", nil), &match) {
		t.Fatal("the URL we register with Telegram does not match the route we serve")
	}
}
