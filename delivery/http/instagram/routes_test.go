package instagram

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

type recordingAC struct {
	calls map[string]string
}

func (r *recordingAC) fn(resource workspace_domain.Resource, action workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.calls[req.Method+" "+req.URL.Path] = string(resource) + ":" + string(action)
		h(w, req)
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
		{http.MethodGet, "/oauth/instagram/start", "instagram_accounts:create"},
		{http.MethodGet, "/instagram/accounts", "instagram_accounts:read"},
		{http.MethodGet, "/instagram/accounts/acct-1", "instagram_accounts:read"},
		{http.MethodPut, "/instagram/accounts/acct-1", "instagram_accounts:update"},
		{http.MethodDelete, "/instagram/accounts/acct-1", "instagram_accounts:delete"},
		{http.MethodGet, "/instagram/accounts/acct-1/media", "instagram_accounts:read"},
		{http.MethodPost, "/instagram/accounts/acct-1/media", "instagram_accounts:update"},
		{http.MethodGet, "/instagram/accounts/acct-1/media/m-1", "instagram_accounts:read"},
		{http.MethodPatch, "/instagram/accounts/acct-1/media/m-1", "instagram_accounts:update"},
		{http.MethodGet, "/instagram/accounts/acct-1/media/m-1/asset", "instagram_accounts:read"},
		{http.MethodGet, "/instagram/accounts/acct-1/media/m-1/comments", "instagram_accounts:read"},
		{http.MethodPost, "/instagram/accounts/acct-1/comments/c-1/replies", "instagram_accounts:update"},
		{http.MethodPost, "/instagram/accounts/acct-1/comments/c-1/hide", "instagram_accounts:update"},
		{http.MethodPost, "/instagram/accounts/acct-1/comments/c-1/private-reply", "instagram_accounts:update"},
		{http.MethodDelete, "/instagram/accounts/acct-1/comments/c-1", "instagram_accounts:update"},
	}

	for _, c := range cases {
		var match mux.RouteMatch
		req := httptest.NewRequest(c.method, c.path, nil)

		if !router.Match(req, &match) {
			t.Errorf("%s %s is not registered", c.method, c.path)
			continue
		}

		func() {
			defer func() { _ = recover() }()
			match.Handler.ServeHTTP(httptest.NewRecorder(), req)
		}()

		got := ac.calls[c.method+" "+c.path]
		if got != c.want {
			t.Errorf("%s %s guarded by %q, want %q", c.method, c.path, got, c.want)
		}
	}
}

func TestRegisterPublicRoutes_OnlyTheTwoUnauthenticatedEndpoints(t *testing.T) {
	router := mux.NewRouter()
	RegisterPublicRoutes(router, &Handler{}, &WebhookHandler{})

	public := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/oauth/instagram/callback"},
		{http.MethodGet, "/oauth/instagram/callback/"},
		{http.MethodGet, "/webhooks/instagram"},
		{http.MethodPost, "/webhooks/instagram"},
	}
	for _, c := range public {
		var match mux.RouteMatch
		if !router.Match(httptest.NewRequest(c.method, c.path, nil), &match) {
			t.Errorf("%s %s is not registered as public", c.method, c.path)
		}
	}

	for _, path := range []string{"/instagram/accounts", "/oauth/instagram/start"} {
		var match mux.RouteMatch
		if router.Match(httptest.NewRequest(http.MethodGet, path, nil), &match) {
			t.Errorf("%s must not be a public route", path)
		}
	}
}

func TestRegisterRoutes_NilHandlersRegisterNothing(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

	RegisterProtectedRoutes(router, nil, ac.fn)
	RegisterPublicRoutes(router, nil, nil)

	for _, path := range []string{"/webhooks/instagram", "/instagram/accounts", "/oauth/instagram/callback"} {
		var match mux.RouteMatch
		if router.Match(httptest.NewRequest(http.MethodGet, path, nil), &match) {
			t.Errorf("nil handlers still registered %s", path)
		}
	}
}
