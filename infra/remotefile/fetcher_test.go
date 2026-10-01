package remotefile

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vozko/infra/netguard"
)

func TestAFileWithinTheLimitIsReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("thumbnail"))
	}))
	defer server.Close()
	data, err := NewFetcher(server.Client(), 64).Fetch(context.Background(), server.URL)
	if err != nil || string(data) != "thumbnail" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

func TestAnOversizedOrFailedFileIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer server.Close()
	fetcher := NewFetcher(server.Client(), 64)
	if _, err := fetcher.Fetch(context.Background(), server.URL+"/big"); err == nil {
		t.Fatal("a file over the limit must be refused")
	}
	if _, err := fetcher.Fetch(context.Background(), server.URL+"/gone"); err == nil {
		t.Fatal("an error status must be refused")
	}
}

func TestWithTheGuardedClientInternalAddressesAreRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()
	_, err := NewFetcher(netguard.NewHTTPClient(2*time.Second), 64).Fetch(context.Background(), server.URL)
	if !errors.Is(err, netguard.ErrBlockedAddress) {
		t.Fatalf("err = %v", err)
	}
}
