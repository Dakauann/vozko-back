package netguard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTheGuardedClientRefusesToReachInsideTheNetwork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, err := NewHTTPClient(2 * time.Second).Get(server.URL)
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("a loopback server must be refused at dial time, got %v", err)
	}

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("the plain client reaches the same server, so the refusal comes from the guard: %v", err)
	}
	_ = resp.Body.Close()
}

func TestTheGuardedClientIgnoresProxySettings(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	transport, ok := NewHTTPClient(time.Second).Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("a proxy would dial on the guard's behalf and skip the address check")
	}
}
