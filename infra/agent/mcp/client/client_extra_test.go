package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newRPCErr(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "error": map[string]any{"code": -1, "message": "no"}})
	}))
}

func TestListToolsErr(t *testing.T) {
	srv := newRPCErr(t)
	defer srv.Close()
	c := New(srv.URL)
	if _, err := c.ListTools(context.Background()); err == nil {
		t.Fatal()
	}
}

func TestCallToolErr(t *testing.T) {
	srv := newRPCErr(t)
	defer srv.Close()
	c := New(srv.URL)
	if _, err := c.CallTool(context.Background(), "x", nil); err == nil {
		t.Fatal()
	}
}

func TestInitializeErr(t *testing.T) {
	srv := newRPCErr(t)
	defer srv.Close()
	c := New(srv.URL)
	if err := c.Initialize(context.Background(), "n", "v"); err == nil {
		t.Fatal()
	}
}
