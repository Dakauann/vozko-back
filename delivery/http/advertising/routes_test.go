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
	{http.MethodPost, "/ads/accounts/acc-1/pages/page-1/whatsapp-link/code", "ads:update"},
	{http.MethodPost, "/ads/accounts/acc-1/pages/page-1/whatsapp-link", "ads:update"},
	{http.MethodGet, "/ads/accounts", "ads:read"},
	{http.MethodPost, "/ads/accounts/a-1/sync", "ads:read"},
	{http.MethodGet, "/ads/accounts/a-1/readiness", "ads:read"},
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
	{http.MethodGet, "/ads/accounts/a-1/pages/p-1/posts/p-1_9", "ads:read"},
	{http.MethodGet, "/ads/accounts/a-1/pages/p-1/instant-experiences", "ads:create"},
	{http.MethodGet, "/ads/accounts/a-1/pixels", "ads:read"},
	{http.MethodPost, "/ads/accounts/a-1/pixels", "ads:create"},
	{http.MethodGet, "/ads/accounts/a-1/budget-minimum", "ads:read"},
	{http.MethodPost, "/ads/objects/bulk/activate", "ads:start"},
	{http.MethodPost, "/ads/objects/bulk/pause", "ads:stop"},
	{http.MethodPost, "/ads/objects/bulk/edit", "ads:update"},
	{http.MethodPost, "/ads/objects/bulk/apply", "ads:update"},
	{http.MethodPost, "/ads/objects/o-1/activate", "ads:start"},
	{http.MethodPost, "/ads/objects/o-1/pause", "ads:stop"},
	{http.MethodPatch, "/ads/objects/o-1/budget", "ads:update"},
	{http.MethodGet, "/ads/objects/o-1", "ads:read"},
	{http.MethodPatch, "/ads/objects/o-1", "ads:update"},
	{http.MethodPost, "/ads/objects/o-1/copies", "ads:create"},
	{http.MethodPost, "/ads/objects/o-1/archive", "ads:update"},
	{http.MethodDelete, "/ads/objects/o-1", "ads:delete"},
	{http.MethodGet, "/ads/report-options", "ads:read"},
	{http.MethodGet, "/ads/reports", "ads:read"},
	{http.MethodPost, "/ads/reports", "ads:create"},
	{http.MethodGet, "/ads/reports/r-1", "ads:read"},
	{http.MethodPut, "/ads/reports/r-1", "ads:update"},
	{http.MethodDelete, "/ads/reports/r-1", "ads:delete"},
	{http.MethodPost, "/ads/accounts/a-1/report-runs", "ads:read"},
	{http.MethodPost, "/ads/report-exports", "ads:read"},
	{http.MethodGet, "/ads/report-exports", "ads:read"},
	{http.MethodGet, "/ads/report-exports/e-1/file", "ads:read"},
	{http.MethodDelete, "/ads/report-exports/e-1", "ads:delete"},
	{http.MethodGet, "/ads/accounts/a-1/tests", "ads:read"},
	{http.MethodPost, "/ads/tests", "ads:create"},
	{http.MethodGet, "/ads/accounts/a-1/drafts", "ads:read"},
	{http.MethodPost, "/ads/accounts/a-1/drafts/discard", "ads:create"},
	{http.MethodPost, "/ads/drafts", "ads:create"},
	{http.MethodGet, "/ads/drafts/d-1", "ads:read"},
	{http.MethodPut, "/ads/drafts/d-1", "ads:create"},
	{http.MethodDelete, "/ads/drafts/d-1", "ads:create"},
	{http.MethodPost, "/ads/drafts/d-1/publish", "ads:create"},
	{http.MethodPost, "/ads/drafts/d-1/copies", "ads:create"},
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
	{http.MethodPost, "/ads/publish-jobs/j-1/activate", "ads:start"},
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

func TestTheCustomerListAlsoNeedsToReadLeads(t *testing.T) {
	var gates []string
	chain := func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, req *http.Request) {
			gates = append(gates, string(resource)+":"+string(action))
			if resource != workspace_domain.ResourceLeads {
				next(w, req)
			}
		}
	}
	router := mux.NewRouter()
	RegisterProtectedRoutes(router, &Handler{}, chain)
	req := httptest.NewRequest(http.MethodPost, "/ads/audiences/customer-list", nil)
	var match mux.RouteMatch
	if !router.Match(req, &match) {
		t.Fatal("the customer list route is not registered")
	}
	match.Handler.ServeHTTP(httptest.NewRecorder(), req)
	if len(gates) != 2 || gates[0] != "ads:create" || gates[1] != "leads:read" {
		t.Fatalf("gates = %v", gates)
	}
}
