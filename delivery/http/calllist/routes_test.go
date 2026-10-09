package calllisthttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

const (
	someList = "11111111-1111-4111-8111-111111111111"
	someItem = "22222222-2222-4222-8222-222222222222"
)

func TestEveryCallListRouteIsGuardedByItsPermission(t *testing.T) {
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
		{http.MethodGet, "/call-lists", "call_lists:read"},
		{http.MethodGet, "/call-lists/" + someList, "call_lists:read"},
		{http.MethodPatch, "/call-lists/" + someList, "call_lists:manage"},
		{http.MethodDelete, "/call-lists/" + someList, "call_lists:manage"},
		{http.MethodGet, "/call-lists/" + someList + "/items", "call_lists:read"},
		{http.MethodPost, "/call-lists/" + someList + "/next", "call_lists:read"},
		{http.MethodPost, "/call-list-items/" + someItem + "/release", "call_lists:read"},
		{http.MethodPost, "/call-list-items/" + someItem + "/close", "call_lists:read"},
	}
	for _, c := range cases {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(c.method, c.path, nil))
		if got := granted[c.method+" "+c.path]; got != c.want {
			t.Errorf("%s %s guarded by %q, want %q", c.method, c.path, got, c.want)
		}
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/call-lists/not-a-uuid", nil))
	if recorder.Code != http.StatusNotFound || len(granted) != len(cases) {
		t.Fatalf("a list id that is not a uuid reached a handler: %d", recorder.Code)
	}
}
