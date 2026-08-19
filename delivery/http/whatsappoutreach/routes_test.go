package whatsappoutreach

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

// recordingAC captures the RBAC resource/action each route was registered with,
// so the test asserts authorization is actually applied rather than just that a
// path exists. Copied from the unofficial channel's harness on purpose: the two
// features make the same promise and should be checked the same way.
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

// The gate is the feature's whole safety story on the HTTP side: this endpoint
// spends the workspace's balance, and the permission it demands must not be
// quietly loosened to one every attendant already holds.
func TestRegisterProtectedRoutes_AppliesRBAC(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

	// A zero handler is enough: this exercises registration, not behaviour.
	RegisterProtectedRoutes(router, &Handler{}, ac.fn)

	cases := []struct {
		method string
		path   string
		want   string
		why    string
	}{
		{
			method: http.MethodPost,
			path:   "/whatsapp/outreach/conversations",
			want:   "whatsapp_templates:send",
			why:    "cold outbound spends money and must not ride on templates:read or conversations:send",
		},
		{
			method: http.MethodGet,
			path:   "/whatsapp/outreach/quote",
			want:   "whatsapp_templates:read",
			why:    "showing a price must not require the permission to spend it",
		},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			got, ok := ac.calls[tc.method+" "+tc.path]
			if !ok {
				t.Fatalf("route not registered: %s %s", tc.method, tc.path)
			}
			if got != tc.want {
				t.Fatalf("gate = %q, want %q — %s", got, tc.want, tc.why)
			}
		})
	}
}

// A channel that is not wired registers nothing, rather than routes that would
// nil-panic on the first request. For this feature that is also the safer
// failure: absent beats present-and-half-wired when money is involved.
func TestRegisterProtectedRoutes_NilHandlerRegistersNothing(t *testing.T) {
	router := mux.NewRouter()
	ac := &recordingAC{calls: map[string]string{}}

	RegisterProtectedRoutes(router, nil, ac.fn)

	req := httptest.NewRequest(http.MethodPost, "/whatsapp/outreach/conversations", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when the feature is not wired", rec.Code)
	}
	if len(ac.calls) != 0 {
		t.Fatalf("no route should have been registered, got %v", ac.calls)
	}
}
