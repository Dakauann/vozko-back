package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newServer(handler func(req map[string]any) (any, *struct {
	Code    int
	Message string
})) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		result, errObj := handler(req)
		out := map[string]any{"jsonrpc": "2.0", "id": req["id"]}
		if errObj != nil {
			out["error"] = errObj
		} else {
			out["result"] = result
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
}

func TestInitialize(t *testing.T) {
	srv := newServer(func(req map[string]any) (any, *struct {
		Code    int
		Message string
	}) {

		switch req["method"] {
		case "initialize", "notifications/initialized":
		default:
			t.Errorf("method=%v", req["method"])
		}
		return map[string]any{}, nil
	})
	defer srv.Close()
	c := New(srv.URL)
	if err := c.Initialize(context.Background(), "vozko", "1.0"); err != nil {
		t.Fatal(err)
	}
}

func TestListToolsAndAPIKeyHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "magickey" {
			http.Error(w, "no key", 401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": 2,
			"result": map[string]any{"tools": []map[string]any{
				{"name": "search", "title": "T", "description": "D", "inputSchema": map[string]any{"type": "object"}},
			}},
		})
	}))
	defer srv.Close()
	c := New(srv.URL)
	c.HeaderName = "X-API-Key"
	c.Prefix = ""
	c.APIKey = "magickey"
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "search" || !strings.Contains(string(tools[0].InputSchema), "object") {
		t.Fatalf("%+v", tools)
	}
}

func TestCallToolSuccess(t *testing.T) {
	srv := newServer(func(req map[string]any) (any, *struct {
		Code    int
		Message string
	}) {
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": "hi"}},
			"isError": false,
		}, nil
	})
	defer srv.Close()
	c := New(srv.URL)
	res, err := c.CallTool(context.Background(), "search", map[string]any{"q": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 1 || res.Content[0].Text != "hi" {
		t.Fatalf("%+v", res)
	}
}

func TestCallToolEmptyContent(t *testing.T) {
	srv := newServer(func(req map[string]any) (any, *struct {
		Code    int
		Message string
	}) {
		return map[string]any{"content": []map[string]any{}}, nil
	})
	defer srv.Close()
	c := New(srv.URL)
	if _, err := c.CallTool(context.Background(), "x", nil); err == nil {
		t.Fatal("expected empty error")
	}
}

func TestOptionsAuthorizationDefault(t *testing.T) {
	c := New("http://x")
	c.APIKey = "k"
	o := c.options(1)
	if o.ExtraHeader["Authorization"] != "Bearer k" {
		t.Fatalf("got %q", o.ExtraHeader["Authorization"])
	}
}

func TestOptionsBearer(t *testing.T) {
	c := New("http://x")
	c.BearerToken = "tok"
	o := c.options(1)
	if o.BearerToken != "tok" {
		t.Fatal("missing bearer")
	}
}
