package carriercampaign

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func TestEveryProtectedRouteIsGuarded(t *testing.T) {
	router := mux.NewRouter()
	guards := map[string]string{}
	RegisterProtectedRoutes(router, &Handler{}, func(res workspace_domain.Resource, action workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			guards[r.Method+" "+r.URL.Path] = string(res) + ":" + string(action)
		}
	})
	carrier := string(workspace_domain.ResourceCarrierMessaging)
	cases := []struct {
		method, path, want string
	}{
		{http.MethodGet, "/carrier/campaigns", carrier + ":read"},
		{http.MethodPost, "/carrier/campaigns", carrier + ":create"},
		{http.MethodGet, "/carrier/campaigns/c1", carrier + ":read"},
		{http.MethodPatch, "/carrier/campaigns/c1", carrier + ":update"},
		{http.MethodDelete, "/carrier/campaigns/c1", carrier + ":delete"},
		{http.MethodPost, "/carrier/campaigns/c1/duplicate", carrier + ":create"},
		{http.MethodPost, "/carrier/campaigns/c1/estimate", carrier + ":update"},
		{http.MethodPost, "/carrier/campaigns/c1/test", carrier + ":update"},
		{http.MethodPost, "/carrier/campaigns/c1/launch", carrier + ":start"},
		{http.MethodPost, "/carrier/campaigns/c1/ready", carrier + ":start"},
		{http.MethodPost, "/carrier/campaigns/c1/resume", carrier + ":start"},
		{http.MethodPost, "/carrier/campaigns/c1/pause", carrier + ":stop"},
		{http.MethodPost, "/carrier/campaigns/c1/stop", carrier + ":stop"},
		{http.MethodGet, "/carrier/campaigns/c1/funnel", carrier + ":read"},
		{http.MethodGet, "/carrier/campaigns/c1/variants", carrier + ":read"},
		{http.MethodGet, "/carrier/campaigns/c1/buttons", carrier + ":read"},
		{http.MethodGet, "/carrier/campaigns/c1/carriers", carrier + ":read"},
		{http.MethodGet, "/carrier/campaigns/c1/timeline", carrier + ":read"},
		{http.MethodGet, "/carrier/campaigns/c1/recipients", carrier + ":read"},
		{http.MethodGet, "/carrier/overview", carrier + ":read"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		var match mux.RouteMatch
		if !router.Match(req, &match) {
			t.Errorf("%s %s is not registered", tc.method, tc.path)
			continue
		}
		match.Handler.ServeHTTP(httptest.NewRecorder(), req)
		if got := guards[tc.method+" "+tc.path]; got != tc.want {
			t.Errorf("%s %s guarded by %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestRegisterNeedsHandlerAndGuard(t *testing.T) {
	router := mux.NewRouter()
	RegisterProtectedRoutes(router, nil, nil)
	var match mux.RouteMatch
	if router.Match(httptest.NewRequest(http.MethodGet, "/carrier/campaigns", nil), &match) {
		t.Fatal("no routes without a handler and a guard")
	}
}
