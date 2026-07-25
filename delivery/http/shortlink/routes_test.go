package shortlink

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

type passthroughLimiter struct{}

func (passthroughLimiter) Validate(next http.Handler) http.Handler { return next }

func TestRegisterRoutes(t *testing.T) {
	h := defaultDeps().build()
	r := mux.NewRouter()
	ac := func(res workspace_domain.Resource, act workspace_domain.Action, hf http.HandlerFunc) http.HandlerFunc {
		return hf
	}
	RegisterRoutes(r, h, ac)

	for _, path := range []string{"/short-links", "/short-links/stats", "/short-links/id-1", "/short-links/id-1/analytics", "/short-links/id-1/clicks", "/short-links/id-1/qr"} {
		var match mux.RouteMatch
		if !r.Match(httptest.NewRequest(http.MethodGet, path, nil), &match) {
			t.Fatalf("route not registered: %s", path)
		}
	}
}

func TestRegisterPublicRoutes(t *testing.T) {
	h := defaultDeps().build()
	r := mux.NewRouter()
	RegisterPublicRoutes(r, h, passthroughLimiter{})

	var match mux.RouteMatch
	if !r.Match(httptest.NewRequest(http.MethodGet, "/r/abc", nil), &match) {
		t.Fatal("redirect route not registered")
	}
	var unlockMatch mux.RouteMatch
	if !r.Match(httptest.NewRequest(http.MethodPost, "/r/abc/unlock", nil), &unlockMatch) {
		t.Fatal("unlock route not registered")
	}
}
