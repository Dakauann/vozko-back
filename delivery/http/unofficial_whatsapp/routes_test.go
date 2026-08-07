package unofficial_whatsapp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

// recordingAC captures the RBAC resource/action each route was registered with,
// so the test asserts authorization is actually applied rather than just that a
// path exists.
type recordingAC struct {
	calls map[string]string
}

func (r *recordingAC) fn(resource workspace_domain.Resource, action workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.calls[req.Method+" "+req.URL.Path] = string(resource) + ":" + string(action)
		// The downstream handler is deliberately NOT invoked: this asserts
		// registration and guarding, and running the body would only exercise a
		// zero-valued usecase graph.
		if h == nil {
			http.Error(w, "handler must not be nil", http.StatusInternalServerError)
		}
	}
}

func TestRegisterProtectedRoutes_AppliesRBAC(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

	// A zero handler is enough: this exercises registration, not behaviour.
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

		// Linking is an UPDATE, not a CREATE: the slot already exists by then,
		// and an attendant allowed to reconnect a dropped session must not
		// thereby be able to provision new numbers against the workspace's
		// capacity.
		{http.MethodPost, "/unofficial-whatsapp/instances/i-1/connect", res + ":update"},
		{http.MethodGet, "/unofficial-whatsapp/instances/i-1/link-status", res + ":read"},
		{http.MethodPost, "/unofficial-whatsapp/instances/i-1/disconnect", res + ":update"},

		// Repair actions.
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

// A nil handler means the channel is not wired. Registering routes whose
// methods would nil-panic on the first request is worse than having no routes.
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

// Provisioning is workspace RBAC now, not a platform-admin lockout.
//
// The restriction it replaces was explicitly temporary — "until pricing is
// decided" — and its own test asked to be deleted in the change that lifted it.
// This is that change: an allowance granted per workspace, topped up by addons
// and enforced in ProvisionInstanceUseCase, is the pricing the lockout stood in
// for. What is asserted here is only that the ROUTE no longer carries a second
// guard; that the limit is honoured is asserted where the limit lives.
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

// The allowance read must be a READ.
//
// It reports a limit and a usage and changes neither, so gating it on anything
// stronger would hide the number from exactly the attendants who need to know
// why the connect button is disabled.
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

// The group routes carry a SHARPER permission split than the instance ones, and
// this pins it.
//
// Everything here acts inside the customer's own WhatsApp groups, on people who
// are not our users. Removal is ActionDelete rather than ActionUpdate
// deliberately: evicting someone from a group is irreversible from our side and
// visible to everyone in it, and an attendant trusted to invite people is not
// thereby trusted to throw them out. Collapsing the two would make the split in
// the handler advisory.
func TestRegisterGroupRoutes_SplitsReadEditAndEvict(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

	RegisterGroupRoutes(router, &GroupHandler{}, ac.fn)

	const res = "unofficial_whatsapp_instances"
	// The literal JID, not a percent-encoded one. The client encodes it, gorilla
	// matches on the DECODED path, and this map is keyed by URL.Path — which is
	// also decoded. Spelling it encoded here only tested the test.
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

		// The conversation-scoped twins. The CRM holds an entry id and never a
		// WhatsApp JID, and the same act must not become cheaper for being
		// reached through a different id — so the guards are identical.
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

// /instances/allowance must not be swallowed by /instances/{id}.
//
// gorilla matches in registration order and "allowance" is a perfectly good
// value for {id}, so the two routes overlap. Registering them the other way
// round makes the allowance read resolve as "get the instance whose id is
// 'allowance'" — a 404 that looks like the endpoint was never deployed, and one
// nothing in a type checker or a build can catch.
func TestAllowanceRouteIsNotShadowedByTheInstanceLookup(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}
	RegisterProtectedRoutes(router, &Handler{}, ac.fn)

	req := httptest.NewRequest(http.MethodGet, "/unofficial-whatsapp/instances/allowance", nil)
	var match mux.RouteMatch
	if !router.Match(req, &match) {
		t.Fatal("GET /instances/allowance is not registered")
	}

	// The decisive assertion: if {id} had captured it, Vars would carry id.
	if id, captured := match.Vars["id"]; captured {
		t.Errorf("the allowance path was captured by /instances/{id} as id=%q; "+
			"register the literal path first", id)
	}
}
