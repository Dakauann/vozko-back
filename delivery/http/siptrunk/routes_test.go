package siptrunk

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func TestRegisterProtectedRoutesGuardsEveryRouteWithTheRightPermission(t *testing.T) {
	router := mux.NewRouter()
	granted := map[string]string{}
	ac := func(resource workspace_domain.Resource, action workspace_domain.Action, _ http.HandlerFunc) http.HandlerFunc {
		return func(_ http.ResponseWriter, r *http.Request) {
			granted[r.Method+" "+r.URL.Path] = string(resource) + ":" + string(action)
		}
	}
	RegisterProtectedRoutes(router, &Handler{}, ac)

	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodPost, "/sip-trunks", "sip_trunks:create"},
		{http.MethodGet, "/sip-trunks", "sip_trunks:read"},
		{http.MethodGet, "/sip-trunks/t1", "sip_trunks:read"},
		{http.MethodPut, "/sip-trunks/t1", "sip_trunks:update"},
		{http.MethodDelete, "/sip-trunks/t1", "sip_trunks:delete"},
		{http.MethodGet, "/sip-trunks/t1/calls", "sip_trunks:read"},
		{http.MethodDelete, "/sip-trunks/t1/calls/c1", "sip_trunks:call"},
	}
	for _, c := range cases {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(c.method, c.path, nil))
		if got := granted[c.method+" "+c.path]; got != c.want {
			t.Errorf("%s %s guarded by %q, want %q", c.method, c.path, got, c.want)
		}
	}
}
