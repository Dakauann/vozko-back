package unofficial_whatsapp

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

func (r *recordingAC) fn(resource workspace_domain.Resource, action workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.calls[req.Method+" "+req.URL.Path] = string(resource) + ":" + string(action)
		if h == nil {
			http.Error(w, "handler must not be nil", http.StatusInternalServerError)
		}
	}
}

func TestRegisterProtectedRoutes_AppliesRBAC(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

	RegisterProtectedRoutes(router, &Handler{}, ac.fn)

	const res = "unofficial_whatsapp_instances"
	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodPost, "/unofficial-whatsapp/instances", res + ":create"},
		{http.MethodGet, "/unofficial-whatsapp/instances", res + ":read"},
		{http.MethodGet, "/unofficial-whatsapp/instances/i-1", res + ":read"},
		{http.MethodPut, "/unofficial-whatsapp/instances/i-1", res + ":update"},
		{http.MethodDelete, "/unofficial-whatsapp/instances/i-1", res + ":delete"},

		{http.MethodPost, "/unofficial-whatsapp/instances/i-1/connect", res + ":update"},
		{http.MethodGet, "/unofficial-whatsapp/instances/i-1/link-status", res + ":read"},
		{http.MethodPost, "/unofficial-whatsapp/instances/i-1/disconnect", res + ":update"},

		{http.MethodPost, "/unofficial-whatsapp/instances/i-1/reset", res + ":update"},
		{http.MethodPost, "/unofficial-whatsapp/instances/i-1/webhook/rotate", res + ":update"},
	}

	for _, c := range cases {
		var match mux.RouteMatch
		req := httptest.NewRequest(c.method, c.path, nil)

		if !router.Match(req, &match) {
			t.Errorf("%s %s is not registered", c.method, c.path)
			continue
		}

		router.ServeHTTP(httptest.NewRecorder(), req)

		if got := ac.calls[c.method+" "+c.path]; got != c.want {
			t.Errorf("%s %s guarded by %q, want %q", c.method, c.path, got, c.want)
		}
	}
}

func TestRegisterProtectedRoutes_NilHandlerRegistersNothing(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

	RegisterProtectedRoutes(router, nil, ac.fn)

	var match mux.RouteMatch
	req := httptest.NewRequest(http.MethodGet, "/unofficial-whatsapp/instances", nil)
	if router.Match(req, &match) {
		t.Error("no route may be registered when the channel is disabled")
	}
}

func TestCreateInstanceUsesWorkspaceRBACOnly(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}
	RegisterProtectedRoutes(router, &Handler{}, ac.fn)

	const res = "unofficial_whatsapp_instances"
	req := httptest.NewRequest(http.MethodPost, "/unofficial-whatsapp/instances", nil)

	var match mux.RouteMatch
	if !router.Match(req, &match) {
		t.Fatal("POST /instances is not registered")
	}
	router.ServeHTTP(httptest.NewRecorder(), req)

	if got := ac.calls["POST /unofficial-whatsapp/instances"]; got != res+":create" {
		t.Errorf("guarded by %q, want %q", got, res+":create")
	}
}

func TestAllowanceRouteIsARead(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}
	RegisterProtectedRoutes(router, &Handler{}, ac.fn)

	req := httptest.NewRequest(http.MethodGet, "/unofficial-whatsapp/instances/allowance", nil)
	var match mux.RouteMatch
	if !router.Match(req, &match) {
		t.Fatal("GET /instances/allowance is not registered")
	}
	router.ServeHTTP(httptest.NewRecorder(), req)

	want := "unofficial_whatsapp_instances:read"
	if got := ac.calls["GET /unofficial-whatsapp/instances/allowance"]; got != want {
		t.Errorf("guarded by %q, want %q", got, want)
	}
}

func TestRegisterGroupRoutes_SplitsReadEditAndEvict(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

	RegisterGroupRoutes(router, &GroupHandler{}, ac.fn)

	const res = "unofficial_whatsapp_instances"
	const jid = "120363012345678901@g.us"
	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodGet, "/unofficial-whatsapp/instances/i-1/groups", res + ":read"},
		{http.MethodGet, "/unofficial-whatsapp/instances/i-1/groups/" + jid, res + ":read"},
		{http.MethodGet, "/unofficial-whatsapp/instances/i-1/groups/" + jid + "/invite-link", res + ":read"},

		{http.MethodPatch, "/unofficial-whatsapp/instances/i-1/groups/" + jid, res + ":update"},
		{http.MethodPost, "/unofficial-whatsapp/instances/i-1/groups/" + jid + "/participants", res + ":update"},

		{http.MethodDelete, "/unofficial-whatsapp/instances/i-1/groups/" + jid + "/participants", res + ":delete"},
		{http.MethodPost, "/unofficial-whatsapp/instances/i-1/groups/" + jid + "/leave", res + ":delete"},

		{http.MethodGet, "/unofficial-whatsapp/conversations/e-1/group", res + ":read"},
		{http.MethodGet, "/unofficial-whatsapp/conversations/e-1/group/invite-link", res + ":read"},
		{http.MethodPatch, "/unofficial-whatsapp/conversations/e-1/group", res + ":update"},
		{http.MethodPost, "/unofficial-whatsapp/conversations/e-1/group/participants", res + ":update"},
		{http.MethodDelete, "/unofficial-whatsapp/conversations/e-1/group/participants", res + ":delete"},
		{http.MethodPost, "/unofficial-whatsapp/conversations/e-1/group/leave", res + ":delete"},
	}

	for _, c := range cases {
		var match mux.RouteMatch
		req := httptest.NewRequest(c.method, c.path, nil)

		if !router.Match(req, &match) {
			t.Errorf("%s %s is not registered", c.method, c.path)
			continue
		}
		router.ServeHTTP(httptest.NewRecorder(), req)

		if got := ac.calls[c.method+" "+c.path]; got != c.want {
			t.Errorf("%s %s guarded by %q, want %q", c.method, c.path, got, c.want)
		}
	}
}

func TestRegisterGroupRoutes_NilHandlerRegistersNothing(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

	RegisterGroupRoutes(router, nil, ac.fn)

	var match mux.RouteMatch
	req := httptest.NewRequest(http.MethodGet, "/unofficial-whatsapp/instances/i-1/groups", nil)
	if router.Match(req, &match) {
		t.Error("a nil group handler registered routes that would nil-panic on the first request")
	}
}

func TestAllowanceRouteIsNotShadowedByTheInstanceLookup(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}
	RegisterProtectedRoutes(router, &Handler{}, ac.fn)

	req := httptest.NewRequest(http.MethodGet, "/unofficial-whatsapp/instances/allowance", nil)
	var match mux.RouteMatch
	if !router.Match(req, &match) {
		t.Fatal("GET /instances/allowance is not registered")
	}

	if id, captured := match.Vars["id"]; captured {
		t.Errorf("the allowance path was captured by /instances/{id} as id=%q; "+
			"register the literal path first", id)
	}
}
