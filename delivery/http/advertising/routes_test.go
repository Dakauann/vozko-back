package advertisinghttp

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

func (r *recordingAC) fn(resource workspace_domain.Resource, action workspace_domain.Action, _ http.HandlerFunc) http.HandlerFunc {
	return func(_ http.ResponseWriter, req *http.Request) {
		r.calls[req.Method+" "+req.URL.Path] = string(resource) + ":" + string(action)
	}
}

var protectedRoutes = []struct {
	method string
	path   string
	want   string
}{
	{http.MethodGet, "/oauth/meta-ads/start", "ads:create"},
	{http.MethodGet, "/ads/options", "ads:read"},
	{http.MethodGet, "/ads/accounts", "ads:read"},
	{http.MethodPost, "/ads/accounts/a-1/sync", "ads:read"},
	{http.MethodDelete, "/ads/accounts/a-1", "ads:delete"},
	{http.MethodPut, "/ads/accounts/a-1/spend-cap", "ads:update"},
	{http.MethodGet, "/ads/accounts/a-1/report", "ads:read"},
	{http.MethodGet, "/ads/accounts/a-1/report.csv", "ads:read"},
	{http.MethodGet, "/ads/accounts/a-1/trend", "ads:read"},
	{http.MethodGet, "/ads/accounts/a-1/insights", "ads:read"},
	{http.MethodGet, "/ads/accounts/a-1/pages", "ads:read"},
	{http.MethodGet, "/ads/accounts/a-1/locations", "ads:create"},
	{http.MethodGet, "/ads/accounts/a-1/targeting", "ads:create"},
	{http.MethodPost, "/ads/accounts/a-1/reach-estimate", "ads:create"},
	{http.MethodGet, "/ads/accounts/a-1/catalogs", "ads:create"},
	{http.MethodGet, "/ads/accounts/a-1/apps", "ads:create"},
	{http.MethodGet, "/ads/accounts/a-1/pages/p-1/posts", "ads:create"},
	{http.MethodGet, "/ads/accounts/a-1/pages/p-1/instant-experiences", "ads:create"},
	{http.MethodGet, "/ads/accounts/a-1/pixels", "ads:read"},
	{http.MethodPost, "/ads/accounts/a-1/pixels", "ads:create"},
	{http.MethodPost, "/ads/objects/o-1/activate", "ads:start"},
	{http.MethodPost, "/ads/objects/o-1/pause", "ads:stop"},
	{http.MethodPatch, "/ads/objects/o-1/budget", "ads:update"},
	{http.MethodGet, "/ads/objects/o-1", "ads:read"},
	{http.MethodPatch, "/ads/objects/o-1", "ads:update"},
	{http.MethodPost, "/ads/objects/o-1/copies", "ads:create"},
	{http.MethodPost, "/ads/objects/o-1/archive", "ads:update"},
	{http.MethodDelete, "/ads/objects/o-1", "ads:delete"},
	{http.MethodGet, "/ads/accounts/a-1/tests", "ads:read"},
	{http.MethodPost, "/ads/tests", "ads:create"},
	{http.MethodGet, "/ads/accounts/a-1/audiences", "ads:read"},
	{http.MethodPost, "/ads/audiences/customer-list", "ads:create"},
	{http.MethodPost, "/ads/audiences/lookalike", "ads:create"},
	{http.MethodDelete, "/ads/audiences/au-1", "ads:delete"},
	{http.MethodGet, "/ads/saved-audiences", "ads:read"},
	{http.MethodPost, "/ads/saved-audiences", "ads:create"},
	{http.MethodPut, "/ads/saved-audiences/s-1", "ads:update"},
	{http.MethodDelete, "/ads/saved-audiences/s-1", "ads:delete"},
	{http.MethodGet, "/ads/accounts/a-1/pages/p-1/forms", "ads:read"},
	{http.MethodPost, "/ads/forms", "ads:create"},
	{http.MethodPost, "/ads/forms/f-1/archive", "ads:update"},
	{http.MethodGet, "/ads/forms/f-1/leads", "ads:read"},
	{http.MethodPost, "/ads/forms/f-1/sync", "ads:read"},
	{http.MethodGet, "/ads/accounts/a-1/rules", "ads:read"},
	{http.MethodPost, "/ads/rules", "ads:create"},
	{http.MethodPost, "/ads/rules/r-1/status", "ads:update"},
	{http.MethodDelete, "/ads/rules/r-1", "ads:delete"},
	{http.MethodGet, "/ads/rules/r-1/history", "ads:read"},
	{http.MethodGet, "/ads/conversions/settings", "ads:read"},
	{http.MethodPut, "/ads/conversions/settings", "ads:update"},
	{http.MethodPost, "/ads/conversions/dataset", "ads:update"},
	{http.MethodGet, "/ads/conversions/recent", "ads:read"},
	{http.MethodPost, "/ads/drafts/validate", "ads:create"},
	{http.MethodPost, "/ads/publish", "ads:create"},
	{http.MethodGet, "/ads/publish-jobs", "ads:read"},
	{http.MethodGet, "/ads/publish-jobs/j-1", "ads:read"},
	{http.MethodPost, "/ads/images", "ads:create"},
	{http.MethodGet, "/ads/conversations/whatsapp/c-1/origin", "ads:read"},
}

func TestEveryProtectedRouteIsGuardedByTheAdsResource(t *testing.T) {
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

func TestNoRoutesWithoutAHandler(t *testing.T) {
	router := mux.NewRouter()
	RegisterProtectedRoutes(router, nil, (&recordingAC{calls: map[string]string{}}).fn)
	RegisterPublicRoutes(router, nil)
	RegisterWebhookRoute(router, nil)
	var match mux.RouteMatch
	for _, path := range []string{"/ads/accounts", "/oauth/meta-ads/callback", "/webhooks/meta-ads"} {
		if router.Match(httptest.NewRequest(http.MethodGet, path, nil), &match) {
			t.Errorf("%s registered without a handler", path)
		}
	}
}

func TestPublicRoutesAreOnlyTheCallbackAndTheWebhook(t *testing.T) {
	router := mux.NewRouter()
	RegisterPublicRoutes(router, &Handler{})
	RegisterWebhookRoute(router, &metawebhook.Handler{})

	public := []struct{ method, path string }{
		{http.MethodGet, "/oauth/meta-ads/callback"},
		{http.MethodGet, "/oauth/meta-ads/callback/"},
		{http.MethodGet, "/webhooks/meta-ads"},
		{http.MethodPost, "/webhooks/meta-ads"},
	}
	for _, c := range public {
		var match mux.RouteMatch
		if !router.Match(httptest.NewRequest(c.method, c.path, nil), &match) {
			t.Errorf("%s %s should be public", c.method, c.path)
		}
	}
	var match mux.RouteMatch
	if router.Match(httptest.NewRequest(http.MethodPut, "/webhooks/meta-ads", nil), &match) && match.MatchErr == nil {
		t.Fatal("the webhook accepts only GET and POST")
	}
	if router.Match(httptest.NewRequest(http.MethodGet, "/ads/accounts", nil), &match) {
		t.Fatal("management routes must not be public")
	}
}
