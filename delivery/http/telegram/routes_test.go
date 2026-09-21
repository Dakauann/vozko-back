package telegram

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	tgdomain "vozko/domain/telegram"
	workspace_domain "vozko/domain/workspace"
)

type recordingAC struct {
	calls map[string]string
}

func (r *recordingAC) fn(resource workspace_domain.Resource, action workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.calls[req.Method+" "+req.URL.Path] = string(resource) + ":" + string(action)
		if h == nil {
			t := "handler must not be nil"
			http.Error(w, t, http.StatusInternalServerError)
		}
	}
}

func TestRegisterProtectedRoutes_AppliesRBAC(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

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

func TestRegisterProtectedRoutes_NilHandlerRegistersNothing(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

	RegisterProtectedRoutes(router, nil, ac.fn)

	var match mux.RouteMatch
	if router.Match(httptest.NewRequest(http.MethodGet, "/telegram/accounts", nil), &match) {
		t.Error("no route may be registered when the channel is disabled")
	}
}

func TestRegisterPublicRoutes(t *testing.T) {
	router := mux.NewRouter()
	RegisterPublicRoutes(router, NewWebhookHandler(&stubAccounts{}, &stubPublisher{}))

	var match mux.RouteMatch
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram/acct-1", nil)
	if !router.Match(req, &match) {
		t.Fatal("the webhook route is not registered on the public router")
	}
	if got := match.Vars["accountId"]; got != "acct-1" {
		t.Errorf("accountId var = %q, want acct-1, tenancy comes from the URL", got)
	}

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
