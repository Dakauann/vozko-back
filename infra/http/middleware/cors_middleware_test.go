package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS_TrustedOrigin_Preflight(t *testing.T) {
	cors := NewCORSMiddleware([]string{"https://app.vozko.com.br", "http://localhost:3000"})
	handler := cors.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream handler should not be called on preflight")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/agents", nil)
	req.Header.Set("Origin", "https://app.vozko.com.br")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://app.vozko.com.br" {
		t.Errorf("expected origin echo, got %q", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("expected credentials=true, got %q", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("expected Allow-Headers to be set")
	}
	if got := rr.Header().Get("Access-Control-Max-Age"); got != "86400" {
		t.Errorf("expected max-age 86400, got %q", got)
	}
}

func TestCORS_TrustedOrigin_ActualRequest(t *testing.T) {
	cors := NewCORSMiddleware([]string{"http://localhost:3000"})
	called := false
	handler := cors.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("downstream handler should be called for non-preflight")
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("expected origin echo, got %q", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("expected credentials=true, got %q", got)
	}
}

func TestCORS_UntrustedOrigin_GetsWildcard(t *testing.T) {
	cors := NewCORSMiddleware([]string{"https://app.vozko.com.br"})
	handler := cors.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	req.Header.Set("Origin", "https://third-party.example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("expected *, got %q", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("expected no credentials header for wildcard, got %q", got)
	}
}

func TestCORS_NoOrigin_NoHeaders(t *testing.T) {
	cors := NewCORSMiddleware([]string{"https://app.vozko.com.br"})
	called := false
	handler := cors.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/asaas", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("downstream should be called")
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS headers for server-to-server, got %q", got)
	}
}

func TestCORS_UntrustedOrigin_PreflightNoCredentials(t *testing.T) {
	cors := NewCORSMiddleware([]string{"https://app.vozko.com.br"})
	handler := cors.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be called on preflight")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/auth/login", nil)
	req.Header.Set("Origin", "https://some-other-app.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("expected *, got %q", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("untrusted preflight should not have credentials, got %q", got)
	}
}

func TestCORS_TrailingSlashNormalization(t *testing.T) {
	cors := NewCORSMiddleware([]string{"https://app.vozko.com.br/"})
	handler := cors.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	req.Header.Set("Origin", "https://app.vozko.com.br")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://app.vozko.com.br" {
		t.Errorf("trailing slash mismatch: expected origin echo, got %q", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("expected credentials=true, got %q", got)
	}
}
