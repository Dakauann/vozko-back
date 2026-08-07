package unofficial_whatsapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
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

// TestCreateInstanceIsPlatformAdminOnly pins the temporary provisioning gate.
//
// Delete this test in the same change that lifts the restriction; a passing
// test for a rule nobody wants any more is how the rule survives its own
// deprecation.
func TestCreateInstanceIsPlatformAdminOnly(t *testing.T) {
	reached := false
	guarded := requirePlatformAdmin(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusCreated)
	})

	cases := []struct {
		name     string
		claims   *auth.Claims
		wantCode int
		wantThru bool
	}{
		{"platform admin passes", &auth.Claims{Role: "admin"}, http.StatusCreated, true},
		{"workspace user denied", &auth.Claims{Role: "user"}, http.StatusForbidden, false},
		{"empty role denied", &auth.Claims{Role: ""}, http.StatusForbidden, false},
		// No claims means the request never went through auth. On a provisioning
		// endpoint the safe reading of that wiring fault is "no".
		{"absent claims denied", nil, http.StatusForbidden, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reached = false
			req := httptest.NewRequest(http.MethodPost, "/unofficial-whatsapp/instances", nil)
			if tc.claims != nil {
				req = req.WithContext(context.WithValue(req.Context(), middleware.ClaimsContextKey, tc.claims))
			}
			rec := httptest.NewRecorder()
			guarded(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if reached != tc.wantThru {
				t.Fatalf("handler reached = %v, want %v", reached, tc.wantThru)
			}
		})
	}
}
