package callrouting

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func TestEveryRouteIsGuardedByTheRightPermission(t *testing.T) {
	router := mux.NewRouter()
	granted := map[string]string{}
	ac := func(resource workspace_domain.Resource, action workspace_domain.Action, _ http.HandlerFunc) http.HandlerFunc {
		return func(_ http.ResponseWriter, r *http.Request) {
			granted[r.Method+" "+r.URL.Path] = string(resource) + ":" + string(action)
		}
	}
	RegisterProtectedRoutes(router, &Handler{}, ac)

	cases := []struct {
		method, path, want string
	}{
		{http.MethodPost, "/call-queues", "call_queues:create"},
		{http.MethodGet, "/call-queues", "call_queues:read"},
		{http.MethodGet, "/call-queues/q1", "call_queues:read"},
		{http.MethodPut, "/call-queues/q1", "call_queues:update"},
		{http.MethodDelete, "/call-queues/q1", "call_queues:delete"},
		{http.MethodGet, "/call-queues/transfer-targets", "call_session:transfer"},
		{http.MethodGet, "/call-queues/live", "call_queues:read"},
		{http.MethodGet, "/call-queues/stats", "call_queues:read"},
		{http.MethodGet, "/call-routing/settings", "call_queues:read"},
		{http.MethodPut, "/call-routing/settings", "call_queues:update"},
		{http.MethodGet, "/hold-music/presets", "call_queues:read"},
		{http.MethodGet, "/hold-music/presets/lofi/audio", "call_queues:read"},
	}
	for _, c := range cases {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(c.method, c.path, nil))
		if got := granted[c.method+" "+c.path]; got != c.want {
			t.Errorf("%s %s guarded by %q, want %q", c.method, c.path, got, c.want)
		}
	}
}
