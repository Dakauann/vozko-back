package unofficial_whatsapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

type chainAC struct {
	calls map[string][]string
}

func (c *chainAC) fn(resource workspace_domain.Resource, action workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		key := req.Method + " " + req.URL.Path
		c.calls[key] = append(c.calls[key], string(resource)+":"+string(action))
		h(w, req)
	}
}

func TestRegisterWorkspaceGroupRoutes_AppliesRBAC(t *testing.T) {
	router := mux.NewRouter()
	ac := &chainAC{calls: map[string][]string{}}

	RegisterWorkspaceGroupRoutes(router, &WorkspaceGroupHandler{}, ac.fn)

	const res = "unofficial_whatsapp_groups:"
	const base = "/unofficial-whatsapp/groups"
	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodGet, base, res + "read"},
		{http.MethodGet, base + "/dashboard", res + "read"},
		{http.MethodGet, base + "/membership", res + "read"},
		{http.MethodGet, base + "/people", res + "read"},
		{http.MethodGet, base + "/themes", res + "read"},
		{http.MethodGet, base + "/categories", res + "read"},
		{http.MethodGet, base + "/joins", res + "read"},
		{http.MethodGet, base + "/g-1", res + "read"},
		{http.MethodGet, base + "/g-1/members", res + "read"},
		{http.MethodGet, base + "/g-1/campaigns", res + "read"},
		{http.MethodGet, base + "/g-1/messages", res + "read"},
		{http.MethodGet, base + "/g-1/invite-link", res + "read"},

		{http.MethodPost, base + "/discover", res + "update"},
		{http.MethodPatch, base + "/g-1", res + "update"},
		{http.MethodPost, base + "/g-1/follow", res + "update"},
		{http.MethodPost, base + "/g-1/unfollow", res + "update"},
		{http.MethodPost, base + "/g-1/refresh", res + "update"},

		{http.MethodPost, base, res + "create"},
		{http.MethodPost, base + "/invites/preview", res + "create"},
		{http.MethodPost, base + "/joins", res + "create"},
		{http.MethodPost, base + "/joins/p-1/cancel", res + "create"},
		{http.MethodPost, base + "/g-1/series", res + "create"},
		{http.MethodPost, base + "/g-1/numbers", res + "create"},

		{http.MethodPost, base + "/g-1/leave", res + "delete"},
		{http.MethodPost, base + "/g-1/participants", res + "update"},
		{http.MethodDelete, base + "/g-1/participants", res + "delete"},
		{http.MethodGet, base + "/g-1/members/export", res + "export"},
		{http.MethodPost, base + "/g-1/members/leads", res + "read,leads:read,leads:create"},
		{http.MethodGet, base + "/members/export", res + "export"},
		{http.MethodPost, base + "/people/leads", res + "read,leads:read,leads:create"},

		{http.MethodGet, base + "/analyses", res + "read"},
		{http.MethodGet, base + "/analyses/a-1", res + "read"},
		{http.MethodGet, base + "/analyses/a-1/runs", res + "read"},
		{http.MethodGet, base + "/analyses/a-1/runs/r-1/messages", res + "read"},
		{http.MethodPost, base + "/analyses", res + "configure"},
		{http.MethodPatch, base + "/analyses/a-1", res + "configure"},
		{http.MethodDelete, base + "/analyses/a-1", res + "delete"},
	}

	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		router.ServeHTTP(httptest.NewRecorder(), req)

		got, registered := ac.calls[c.method+" "+c.path]
		if !registered {
			t.Errorf("%s %s is not registered", c.method, c.path)
			continue
		}
		if joined := strings.Join(got, ","); joined != c.want {
			t.Errorf("%s %s guarded by %s, want %s", c.method, c.path, joined, c.want)
		}
	}
}

func TestRegisterWorkspaceGroupRoutes_NilHandlerRegistersNothing(t *testing.T) {
	router := mux.NewRouter()
	RegisterWorkspaceGroupRoutes(router, nil, (&chainAC{calls: map[string][]string{}}).fn)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/unofficial-whatsapp/groups", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when groups are not wired", rec.Code)
	}
}
