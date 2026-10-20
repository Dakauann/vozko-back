package tiktok

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func TestEveryProtectedRouteIsGuardedByTheAccountsResource(t *testing.T) {
	router := mux.NewRouter()
	guards := map[string]string{}
	RegisterProtectedRoutes(router, &Handler{}, func(res workspace_domain.Resource, action workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			guards[r.Method+" "+r.URL.Path] = string(res) + ":" + string(action)
		}
	})
	cases := []struct {
		method, path string
		action       workspace_domain.Action
	}{
		{http.MethodGet, "/oauth/tiktok/start", workspace_domain.ActionCreate},
		{http.MethodGet, "/tiktok/accounts", workspace_domain.ActionRead},
		{http.MethodGet, "/tiktok/accounts/a-1", workspace_domain.ActionRead},
		{http.MethodPut, "/tiktok/accounts/a-1", workspace_domain.ActionUpdate},
		{http.MethodDelete, "/tiktok/accounts/a-1", workspace_domain.ActionDelete},
		{http.MethodPost, "/tiktok/accounts/a-1/health-check", workspace_domain.ActionUpdate},
		{http.MethodGet, "/tiktok/accounts/a-1/videos", workspace_domain.ActionRead},
		{http.MethodGet, "/tiktok/accounts/a-1/videos/v-1/comments", workspace_domain.ActionRead},
		{http.MethodPost, "/tiktok/accounts/a-1/comments/c-1/replies", workspace_domain.ActionUpdate},
		{http.MethodPost, "/tiktok/accounts/a-1/comments/c-1/hide", workspace_domain.ActionUpdate},
		{http.MethodPost, "/tiktok/accounts/a-1/comments/c-1/unhide", workspace_domain.ActionUpdate},
		{http.MethodPost, "/tiktok/accounts/a-1/comments/c-1/like", workspace_domain.ActionUpdate},
		{http.MethodPost, "/tiktok/accounts/a-1/comments/c-1/unlike", workspace_domain.ActionUpdate},
		{http.MethodGet, "/tiktok/accounts/a-1/comment-rules", workspace_domain.ActionRead},
		{http.MethodPost, "/tiktok/accounts/a-1/comment-rules", workspace_domain.ActionUpdate},
		{http.MethodPut, "/tiktok/accounts/a-1/comment-rules/r-1", workspace_domain.ActionUpdate},
		{http.MethodDelete, "/tiktok/accounts/a-1/comment-rules/r-1", workspace_domain.ActionUpdate},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		var match mux.RouteMatch
		if !router.Match(req, &match) {
			t.Errorf("%s %s is not registered", tc.method, tc.path)
			continue
		}
		match.Handler.ServeHTTP(httptest.NewRecorder(), req)
		want := string(workspace_domain.ResourceTikTokAccounts) + ":" + string(tc.action)
		if got := guards[tc.method+" "+tc.path]; got != want {
			t.Errorf("%s %s guarded by %q, want %q", tc.method, tc.path, got, want)
		}
	}
}

func TestThePublicRoutesAreTheCallbackAndTheWebhook(t *testing.T) {
	router := mux.NewRouter()
	RegisterPublicRoutes(router, &Handler{}, &WebhookHandler{})
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/oauth/tiktok/callback/"},
		{http.MethodPost, "/webhooks/tiktok"},
	} {
		var match mux.RouteMatch
		if !router.Match(httptest.NewRequest(tc.method, tc.path, nil), &match) {
			t.Errorf("%s %s is not registered", tc.method, tc.path)
		}
	}
	var match mux.RouteMatch
	if router.Match(httptest.NewRequest(http.MethodGet, "/tiktok/accounts", nil), &match) {
		t.Error("an account route must never be public")
	}
}

func TestTheCallbackPathMatchesTheRedirectURIShape(t *testing.T) {
	if OAuthCallbackPath[len(OAuthCallbackPath)-1] != '/' {
		t.Fatalf("TikTok requires the redirect URI to end with a slash, the callback is %q", OAuthCallbackPath)
	}
}
