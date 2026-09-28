package facebook

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"vozko/delivery/http/metawebhook"
	workspace_domain "vozko/domain/workspace"
)

type recordingAC struct {
	calls map[string]string
}

func (r *recordingAC) fn(resource workspace_domain.Resource, action workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.calls[req.Method+" "+req.URL.Path] = string(resource) + ":" + string(action)
	}
}

var protectedRoutes = []struct {
	method string
	path   string
	want   string
}{
	{http.MethodGet, "/oauth/facebook/start", "facebook_pages:create"},
	{http.MethodGet, "/facebook/pages", "facebook_pages:read"},
	{http.MethodGet, "/facebook/pages/p-1", "facebook_pages:read"},
	{http.MethodPut, "/facebook/pages/p-1", "facebook_pages:update"},
	{http.MethodDelete, "/facebook/pages/p-1", "facebook_pages:delete"},
	{http.MethodPost, "/facebook/pages/p-1/health-check", "facebook_pages:update"},
	{http.MethodGet, "/facebook/pages/p-1/posts", "facebook_pages:read"},
	{http.MethodPost, "/facebook/pages/p-1/posts", "facebook_pages:update"},
	{http.MethodGet, "/facebook/pages/p-1/posts/1_2", "facebook_pages:read"},
	{http.MethodPatch, "/facebook/pages/p-1/posts/1_2", "facebook_pages:update"},
	{http.MethodDelete, "/facebook/pages/p-1/posts/1_2", "facebook_pages:update"},
	{http.MethodGet, "/facebook/pages/p-1/posts/1_2/asset", "facebook_pages:read"},
	{http.MethodGet, "/facebook/pages/p-1/publish-jobs", "facebook_pages:read"},
	{http.MethodGet, "/facebook/pages/p-1/publish-jobs/j-1", "facebook_pages:read"},
	{http.MethodGet, "/facebook/pages/p-1/posts/1_2/comments", "facebook_pages:read"},
	{http.MethodPost, "/facebook/pages/p-1/posts/1_2/comments", "facebook_pages:update"},
	{http.MethodPost, "/facebook/pages/p-1/comments/2_c/replies", "facebook_pages:update"},
	{http.MethodPatch, "/facebook/pages/p-1/comments/2_c", "facebook_pages:update"},
	{http.MethodDelete, "/facebook/pages/p-1/comments/2_c", "facebook_pages:update"},
	{http.MethodPost, "/facebook/pages/p-1/comments/2_c/hide", "facebook_pages:update"},
	{http.MethodPost, "/facebook/pages/p-1/comments/2_c/like", "facebook_pages:update"},
	{http.MethodPost, "/facebook/pages/p-1/comments/2_c/private-reply", "facebook_pages:update"},
	{http.MethodGet, "/facebook/pages/p-1/comment-rules", "facebook_pages:read"},
	{http.MethodPost, "/facebook/pages/p-1/comment-rules", "facebook_pages:update"},
	{http.MethodPut, "/facebook/pages/p-1/comment-rules/r-1", "facebook_pages:update"},
	{http.MethodDelete, "/facebook/pages/p-1/comment-rules/r-1", "facebook_pages:update"},
	{http.MethodGet, "/facebook/pages/p-1/stories", "facebook_pages:read"},
	{http.MethodGet, "/facebook/pages/p-1/messenger-profile", "facebook_pages:read"},
	{http.MethodPut, "/facebook/pages/p-1/messenger-profile", "facebook_pages:update"},
	{http.MethodGet, "/facebook/conversations/c-1/thread", "conversations:read"},
	{http.MethodPost, "/facebook/conversations/c-1/take-control", "conversations:send"},
	{http.MethodPost, "/facebook/conversations/c-1/release-control", "conversations:send"},
}

func TestEveryProtectedRouteIsGuardedByTheFacebookResource(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}
	RegisterProtectedRoutes(router, &Handler{}, ac.fn)

	for _, c := range protectedRoutes {
		req := httptest.NewRequest(c.method, c.path, nil)
		var match mux.RouteMatch
		if !router.Match(req, &match) {
			t.Errorf("%s %s is not registered", c.method, c.path)
			continue
		}
		match.Handler.ServeHTTP(httptest.NewRecorder(), req)
		if got := ac.calls[c.method+" "+c.path]; got != c.want {
			t.Errorf("%s %s guarded by %q, want %q", c.method, c.path, got, c.want)
		}
	}
}

func TestPublicRoutesAreOnlyTheCallbackAndTheWebhook(t *testing.T) {
	router := mux.NewRouter()
	RegisterPublicRoutes(router, &Handler{}, &metawebhook.Handler{})

	public := []struct{ method, path string }{
		{http.MethodGet, "/oauth/facebook/callback"},
		{http.MethodGet, "/webhooks/facebook"},
		{http.MethodPost, "/webhooks/facebook"},
	}
	for _, c := range public {
		var match mux.RouteMatch
		if !router.Match(httptest.NewRequest(c.method, c.path, nil), &match) {
			t.Errorf("%s %s should be public", c.method, c.path)
		}
	}
	var match mux.RouteMatch
	if router.Match(httptest.NewRequest(http.MethodGet, "/facebook/pages", nil), &match) {
		t.Fatal("management routes must not be public")
	}
}
