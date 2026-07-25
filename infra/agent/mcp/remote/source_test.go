package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"vozko/domain/agent/mcp"
)

func newRPCServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		var result any
		switch req["method"] {
		case "initialize":
			result = map[string]any{}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{
				{"name": "search", "inputSchema": map[string]any{"type": "object"}},
			}}
		case "tools/call":
			result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": "ok"}},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": result})
	}))
}

func TestSourceID(t *testing.T) {
	server := &mcp.RemoteMCPServer{ID: "abc", WorkspaceID: "ws", URL: "https://x"}
	s := New(server, "")
	if s.ID() != "remote:abc" || s.Kind() != mcp.KindRemote || s.DisplayName() != "" {
		t.Fatalf("%+v", s)
	}
}

func TestNewWithCredentials(t *testing.T) {
	server := &mcp.RemoteMCPServer{ID: "a", WorkspaceID: "ws", URL: "https://x", Credential: &mcp.Credential{Mode: mcp.AuthAPIKey}}
	s := New(server, "secret")
	if s.client.APIKey != "secret" {
		t.Fatal("api key not set")
	}
	server2 := &mcp.RemoteMCPServer{ID: "a", WorkspaceID: "ws", URL: "https://x", Credential: &mcp.Credential{Mode: mcp.AuthOAuth2}}
	s2 := New(server2, "tok")
	if s2.client.BearerToken != "tok" {
		t.Fatal("bearer not set")
	}
}

func TestListTools(t *testing.T) {
	srv := newRPCServer(t)
	defer srv.Close()
	server := &mcp.RemoteMCPServer{ID: "a", WorkspaceID: "ws", URL: srv.URL, Name: "Remote"}
	s := New(server, "")
	tools, err := s.ListTools(context.Background(), "ws")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("%+v", tools)
	}
}

func TestListToolsWorkspaceMismatch(t *testing.T) {
	server := &mcp.RemoteMCPServer{ID: "a", WorkspaceID: "ws", URL: "https://x"}
	s := New(server, "")
	if _, err := s.ListTools(context.Background(), ""); err == nil {
		t.Fatal("expected workspace required")
	}
	if _, err := s.ListTools(context.Background(), "other"); err == nil {
		t.Fatal("expected scope mismatch")
	}
}

func TestCallTool(t *testing.T) {
	srv := newRPCServer(t)
	defer srv.Close()
	server := &mcp.RemoteMCPServer{ID: "a", WorkspaceID: "ws", URL: srv.URL}
	s := New(server, "")
	res, err := s.CallTool(context.Background(), "ws", "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 1 || res.Content[0].Text != "ok" {
		t.Fatalf("%+v", res)
	}
}

func TestCallToolWorkspaceMismatch(t *testing.T) {
	server := &mcp.RemoteMCPServer{ID: "a", WorkspaceID: "ws", URL: "https://x"}
	s := New(server, "")
	if _, err := s.CallTool(context.Background(), "", "x", nil); err == nil {
		t.Fatal("expected ws required")
	}
	if _, err := s.CallTool(context.Background(), "other", "x", nil); err == nil {
		t.Fatal("expected scope mismatch")
	}
}

func TestInitializeCalledBeforeCallTool(t *testing.T) {
	var initCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req["method"] {
		case "initialize":
			initCalls.Add(1)
			w.Header().Set("Mcp-Session-Id", "test-session-123")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		case "notifications/initialized":
			w.WriteHeader(http.StatusNoContent)
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": req["id"],
				"result": map[string]any{"content": []map[string]any{{"type": "text", "text": "response"}}},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "error": map[string]any{"code": -32601, "message": "not found"}})
		}
	}))
	defer srv.Close()

	server := &mcp.RemoteMCPServer{ID: "test", WorkspaceID: "ws", URL: srv.URL}
	s := New(server, "")

	res, err := s.CallTool(context.Background(), "ws", "test_tool", nil)
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if len(res.Content) != 1 || res.Content[0].Text != "response" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if initCalls.Load() != 1 {
		t.Fatalf("expected 1 initialize call, got %d", initCalls.Load())
	}

	res, err = s.CallTool(context.Background(), "ws", "test_tool", nil)
	if err != nil {
		t.Fatalf("second CallTool failed: %v", err)
	}
	if initCalls.Load() != 1 {
		t.Fatalf("expected initialize to be called only once, got %d", initCalls.Load())
	}
}
