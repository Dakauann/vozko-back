package mcphttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/agent/mcp/vault"
	memrepo "vozko/infra/repositories/agent/mcp"
	ucmcp "vozko/usecases/agent/mcp"
)

type stubResolver struct {
	ws  string
	err error
}

func (s stubResolver) Resolve(_ context.Context, _ string) (string, error) {
	return s.ws, s.err
}

type echoSource struct{}

func (echoSource) ID() string           { return "builtin:echo" }
func (echoSource) Kind() domainmcp.Kind { return domainmcp.KindBuiltin }
func (echoSource) DisplayName() string  { return "echo" }
func (echoSource) ListTools(_ context.Context, _ domainmcp.WorkspaceID) ([]domainmcp.Tool, error) {
	return []domainmcp.Tool{{Name: "say", Title: "Say", Description: "Echo back", InputSchema: []byte(`{"type":"object"}`)}}, nil
}
func (echoSource) CallTool(_ context.Context, _ domainmcp.WorkspaceID, n string, a map[string]any) (domainmcp.ToolResult, error) {
	if n != "say" {
		return domainmcp.ToolResult{}, errors.New("unknown")
	}
	msg, _ := a["msg"].(string)
	return domainmcp.TextResult(msg), nil
}

func newServer(t *testing.T, ws string, resolveErr error) *Server {
	v, _ := vault.New(bytes.Repeat([]byte{1}, 32), 1)
	cat := ucmcp.NewStaticCatalog(domainmcp.BuiltinDescriptor{Key: "echo", Builder: func(*domainmcp.Credential) domainmcp.ToolSource { return echoSource{} }})
	bindings := memrepo.NewBuiltinBindingRepo()
	_ = bindings.Upsert(context.Background(), &domainmcp.BuiltinBinding{ID: "1", WorkspaceID: "ws", ServerKey: "echo", Status: domainmcp.StatusConnected})
	r := ucmcp.NewRegistry(cat, bindings, memrepo.NewRemoteServerRepo(), memrepo.NewToolCacheRepo(), v)
	uc := ucmcp.NewCallToolUseCase(r)
	return New(r, uc, stubResolver{ws: ws, err: resolveErr})
}

func doRPC(t *testing.T, srv *Server, method string, params any, auth string) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	return w.Code, got
}

func TestPublicServerInitialize(t *testing.T) {
	srv := newServer(t, "ws", nil)
	code, body := doRPC(t, srv, "initialize", map[string]any{}, "Bearer x")
	if code != 200 {
		t.Fatalf("code=%d", code)
	}
	res, _ := body["result"].(map[string]any)
	if res["protocolVersion"] != "2025-06-18" {
		t.Fatalf("%+v", res)
	}
}

func TestPublicServerListAndCall(t *testing.T) {
	srv := newServer(t, "ws", nil)
	_, body := doRPC(t, srv, "tools/list", map[string]any{}, "Bearer x")
	res := body["result"].(map[string]any)
	tools := res["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("%+v", tools)
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "builtin:echo.say" {
		t.Fatalf("name=%v", tool["name"])
	}
	_, body = doRPC(t, srv, "tools/call", map[string]any{"name": "builtin:echo.say", "arguments": map[string]any{"msg": "hi"}}, "Bearer x")
	res = body["result"].(map[string]any)
	content := res["content"].([]any)
	if content[0].(map[string]any)["text"] != "hi" {
		t.Fatalf("%+v", content)
	}
}

func TestPublicServerUnknownMethod(t *testing.T) {
	srv := newServer(t, "ws", nil)
	_, body := doRPC(t, srv, "missing", map[string]any{}, "Bearer x")
	if _, ok := body["error"]; !ok {
		t.Fatal()
	}
}

func TestPublicServerCallToolError(t *testing.T) {
	srv := newServer(t, "ws", nil)
	_, body := doRPC(t, srv, "tools/call", map[string]any{"name": "builtin:echo.unknown"}, "Bearer x")
	if _, ok := body["error"]; !ok {
		t.Fatal()
	}
}

func TestPublicServerBadParams(t *testing.T) {
	srv := newServer(t, "ws", nil)
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": "not-object"})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "invalid params") {
		t.Fatalf("body=%s", w.Body.String())
	}
}

func TestPublicServerMethodNotAllowed(t *testing.T) {
	srv := newServer(t, "ws", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code=%d", w.Code)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("missing WWW-Authenticate")
	}
}

func TestPublicServerNoBearer(t *testing.T) {
	srv := newServer(t, "ws", nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatal()
	}
}

func TestPublicServerBadResolver(t *testing.T) {
	srv := newServer(t, "", errors.New("nope"))
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatal()
	}
}

func TestPublicServerEmptyWS(t *testing.T) {
	srv := newServer(t, "", nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatal()
	}
}

func TestPublicServerParseError(t *testing.T) {
	srv := newServer(t, "ws", nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{not-json"))
	req.Header.Set("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "parse error") {
		t.Fatalf("%s", w.Body.String())
	}
}

func TestExtractBearer(t *testing.T) {
	if extractBearer("") != "" {
		t.Fail()
	}
	if extractBearer("Token abc") != "" {
		t.Fail()
	}
	if extractBearer("Bearer xyz") != "xyz" {
		t.Fail()
	}
}

func TestSentinel(t *testing.T) {
	if ErrUnauthorized == nil {
		t.Fail()
	}
}

func TestWWWAuthenticateForwardedProto(t *testing.T) {
	srv := newServer(t, "ws", nil)
	req := httptest.NewRequest(http.MethodGet, "http://localhost/mcp", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if !strings.Contains(w.Header().Get("WWW-Authenticate"), "https") {
		t.Fatalf("header=%s", w.Header().Get("WWW-Authenticate"))
	}
}
