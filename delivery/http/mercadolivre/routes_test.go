package mercadolivre

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

func (r *recordingAC) fn(resource workspace_domain.Resource, action workspace_domain.Action, _ http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.calls[req.Method+" "+req.URL.Path] = string(resource) + ":" + string(action)
	}
}

func TestProtectedRoutesAreGuardedByTheAccountsResource(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}
	RegisterProtectedRoutes(router, &Handler{}, ac.fn)

	cases := []struct {
		method, path, want string
	}{
		{http.MethodGet, "/oauth/mercadolivre/start", "mercadolivre_accounts:create"},
		{http.MethodGet, "/mercadolivre/accounts", "mercadolivre_accounts:read"},
		{http.MethodGet, "/mercadolivre/accounts/acct-1", "mercadolivre_accounts:read"},
		{http.MethodPatch, "/mercadolivre/accounts/acct-1", "mercadolivre_accounts:update"},
		{http.MethodDelete, "/mercadolivre/accounts/acct-1", "mercadolivre_accounts:delete"},
		{http.MethodPost, "/mercadolivre/accounts/acct-1/health-check", "mercadolivre_accounts:update"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		var match mux.RouteMatch
		if !router.Match(req, &match) {
			t.Errorf("%s %s is not registered", c.method, c.path)
			continue
		}
		router.ServeHTTP(httptest.NewRecorder(), req)
		if got := ac.calls[c.method+" "+c.path]; got != c.want {
			t.Errorf("%s %s guarded by %q, want %q", c.method, c.path, got, c.want)
		}
	}
	for _, c := range []struct{ method, path string }{
		{http.MethodPut, "/mercadolivre/accounts/acct-1"},
		{http.MethodGet, "/oauth/mercadolivre/callback"},
		{http.MethodPost, "/webhooks/mercadolivre/token"},
	} {
		var match mux.RouteMatch
		if router.Match(httptest.NewRequest(c.method, c.path, nil), &match) && match.MatchErr == nil {
			t.Errorf("%s %s must not be a protected route", c.method, c.path)
		}
	}
}

func TestOnlyTheCallbackAndTheNotificationsArePublic(t *testing.T) {
	router := mux.NewRouter()
	RegisterPublicRoutes(router, &Handler{}, &NotificationHandler{})
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/oauth/mercadolivre/callback"},
		{http.MethodPost, "/webhooks/mercadolivre/abc"},
	} {
		var match mux.RouteMatch
		if !router.Match(httptest.NewRequest(c.method, c.path, nil), &match) || match.MatchErr != nil {
			t.Errorf("%s %s is not public", c.method, c.path)
		}
	}
	for _, path := range []string{"/mercadolivre/accounts", "/oauth/mercadolivre/start", "/webhooks/mercadolivre"} {
		var match mux.RouteMatch
		if router.Match(httptest.NewRequest(http.MethodGet, path, nil), &match) && match.MatchErr == nil {
			t.Errorf("%s must not be public", path)
		}
	}
}

func TestNilHandlersRegisterNothing(t *testing.T) {
	router := mux.NewRouter()
	RegisterProtectedRoutes(router, nil, (&recordingAC{calls: map[string]string{}}).fn)
	RegisterPublicRoutes(router, nil, nil)
	var match mux.RouteMatch
	if router.Match(httptest.NewRequest(http.MethodGet, "/mercadolivre/accounts", nil), &match) {
		t.Fatal("nil handlers registered routes")
	}
}
