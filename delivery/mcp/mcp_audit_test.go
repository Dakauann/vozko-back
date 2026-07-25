package mcphttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/agent/mcp/vault"
	memrepo "vozko/infra/repositories/agent/mcp"
	ucmcp "vozko/usecases/agent/mcp"
)

func auditServer(t *testing.T) (*Server, *WellKnownProtectedResource) {
	t.Helper()
	v, _ := vault.New(bytes.Repeat([]byte{1}, 32), 1)
	cat := ucmcp.NewStaticCatalog(domainmcp.BuiltinDescriptor{
		Key:     "echo",
		Builder: func(*domainmcp.Credential) domainmcp.ToolSource { return echoSource{} },
	})
	bindings := memrepo.NewBuiltinBindingRepo()
	_ = bindings.Upsert(context.Background(), &domainmcp.BuiltinBinding{
		ID: "1", WorkspaceID: "ws", ServerKey: "echo", Status: domainmcp.StatusConnected,
	})
	r := ucmcp.NewRegistry(cat, bindings, memrepo.NewRemoteServerRepo(), memrepo.NewToolCacheRepo(), v)
	uc := ucmcp.NewCallToolUseCase(r)
	srv := New(r, uc, stubResolver{ws: "ws"})
	meta := &WellKnownProtectedResource{
		Resource:             "https://mcp.vozko.test",
		AuthorizationServers: []string{"https://auth.vozko.test"},
		ScopesSupported:      []string{"mcp.read", "mcp.write"},
	}
	return srv, meta
}

func auditDo(srv http.Handler, method, path, auth, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func auditRPC(srv http.Handler, id any, method string, params any, headers map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	w := auditDo(srv, http.MethodPost, "/mcp", "Bearer t", string(body), headers)
	var env map[string]any
	if len(bytes.TrimSpace(w.Body.Bytes())) > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &env)
	}
	return w, env
}

func TestAudit_JSONRPC_Envelope(t *testing.T) {
	srv, _ := auditServer(t)
	cases := []struct {
		name string
		id   any
	}{
		{"int id", 42},
		{"string id", "abc-123"},
		{"float id", 3.14},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, env := auditRPC(srv, tc.id, "initialize", map[string]any{}, nil)
			if env["jsonrpc"] != "2.0" {
				t.Fatalf("jsonrpc = %v, want \"2.0\"", env["jsonrpc"])
			}
			if env["id"] == nil {
				t.Fatalf("id echoed as null; want %v", tc.id)
			}
		})
	}
}

func TestAudit_JSONRPC_NullIdOnParseError(t *testing.T) {
	srv, _ := auditServer(t)
	w := auditDo(srv, http.MethodPost, "/mcp", "Bearer t", "not-json", nil)
	var env map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if env["id"] != nil {
		t.Fatalf("parse-error id must be null, got %v", env["id"])
	}
	errObj, _ := env["error"].(map[string]any)
	if errObj == nil || errObj["code"].(float64) != -32700 {
		t.Fatalf("expected -32700 Parse error, got %+v", env)
	}
}

func TestAudit_Notifications_NoResponse(t *testing.T) {
	srv, _ := auditServer(t)
	body := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	w := auditDo(srv, http.MethodPost, "/mcp", "Bearer t", body, nil)
	if w.Code != http.StatusAccepted {
		t.Fatalf("notification status = %d, want 202", w.Code)
	}
	if len(bytes.TrimSpace(w.Body.Bytes())) != 0 {
		t.Fatalf("notification response body must be empty; got %q", w.Body.String())
	}
}

func TestAudit_InvalidJSONRPCVersion(t *testing.T) {
	srv, _ := auditServer(t)
	body := `{"jsonrpc":"1.0","id":1,"method":"initialize"}`
	w := auditDo(srv, http.MethodPost, "/mcp", "Bearer t", body, nil)
	var env map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	errObj := env["error"].(map[string]any)
	if errObj["code"].(float64) != -32600 {
		t.Fatalf("want -32600 Invalid Request, got %v", errObj)
	}
}

func TestAudit_Initialize_Result(t *testing.T) {
	srv, _ := auditServer(t)
	_, env := auditRPC(srv, 1, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "audit", "version": "1"},
	}, nil)
	res := env["result"].(map[string]any)
	if res["protocolVersion"] != ProtocolVersion {
		t.Fatalf("protocolVersion = %v, want %s", res["protocolVersion"], ProtocolVersion)
	}
	caps := res["capabilities"].(map[string]any)
	tools, ok := caps["tools"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities.tools missing: %+v", caps)
	}
	if _, ok := tools["listChanged"]; !ok {
		t.Fatalf("capabilities.tools.listChanged required when declaring tools capability")
	}
	info := res["serverInfo"].(map[string]any)
	for _, k := range []string{"name", "version"} {
		if _, ok := info[k]; !ok {
			t.Fatalf("serverInfo missing %q: %+v", k, info)
		}
	}
}

func TestAudit_ProtocolVersionHeader(t *testing.T) {
	srv, _ := auditServer(t)
	t.Run("supported", func(t *testing.T) {
		_, env := auditRPC(srv, 1, "tools/list", map[string]any{},
			map[string]string{"MCP-Protocol-Version": ProtocolVersion})
		if env["error"] != nil {
			t.Fatalf("unexpected error: %+v", env["error"])
		}
	})
	t.Run("unsupported", func(t *testing.T) {
		w, _ := auditRPC(srv, 1, "tools/list", map[string]any{},
			map[string]string{"MCP-Protocol-Version": "1999-01-01"})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("want 400, got %d", w.Code)
		}
	})
	t.Run("absent tolerated", func(t *testing.T) {
		_, env := auditRPC(srv, 1, "tools/list", map[string]any{}, nil)
		if env["error"] != nil {
			t.Fatalf("absent header should be tolerated: %+v", env["error"])
		}
	})
}

func TestAudit_Tool_Schema(t *testing.T) {
	srv, _ := auditServer(t)
	_, env := auditRPC(srv, 1, "tools/list", map[string]any{}, nil)
	res := env["result"].(map[string]any)
	tools := res["tools"].([]any)
	if len(tools) == 0 {
		t.Fatal("no tools")
	}
	for _, raw := range tools {
		tl := raw.(map[string]any)
		if name, _ := tl["name"].(string); name == "" {
			t.Fatalf("tool missing name: %+v", tl)
		}
		schema, ok := tl["inputSchema"].(map[string]any)
		if !ok {
			t.Fatalf("inputSchema must be an object: %+v", tl)
		}
		if schema["type"] != "object" {
			t.Fatalf("inputSchema.type should be \"object\": %+v", schema)
		}
	}
}

func TestAudit_ToolCall_ContentShape(t *testing.T) {
	srv, _ := auditServer(t)
	_, env := auditRPC(srv, 1, "tools/call", map[string]any{
		"name":      "builtin:echo.say",
		"arguments": map[string]any{"msg": "hello"},
	}, nil)
	if env["error"] != nil {
		t.Fatalf("unexpected error: %+v", env["error"])
	}
	res := env["result"].(map[string]any)
	content := res["content"].([]any)
	if len(content) == 0 {
		t.Fatal("content is empty")
	}
	first := content[0].(map[string]any)
	if first["type"] == "" {
		t.Fatalf("content[0].type is required: %+v", first)
	}
	if first["type"] == "text" {
		if _, ok := first["text"].(string); !ok {
			t.Fatalf("text content must carry text: %+v", first)
		}
	}
}

func TestAudit_ToolCall_UnknownTool_Code(t *testing.T) {
	srv, _ := auditServer(t)
	_, env := auditRPC(srv, 1, "tools/call", map[string]any{"name": "builtin:echo.nope"}, nil)
	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got %+v", env)
	}
	if code := errObj["code"].(float64); code != -32602 {
		t.Fatalf("want -32602 Invalid params, got %v (msg=%v)", code, errObj["message"])
	}
}

func TestAudit_MethodNotFound(t *testing.T) {
	srv, _ := auditServer(t)
	_, env := auditRPC(srv, 1, "foo/bar", map[string]any{}, nil)
	errObj := env["error"].(map[string]any)
	if errObj["code"].(float64) != -32601 {
		t.Fatalf("want -32601 Method not found, got %+v", errObj)
	}
}

func TestAudit_InvalidParams(t *testing.T) {
	srv, _ := auditServer(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"not-an-object"}`
	w := auditDo(srv, http.MethodPost, "/mcp", "Bearer t", body, nil)
	var env map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	errObj := env["error"].(map[string]any)
	if errObj["code"].(float64) != -32602 {
		t.Fatalf("want -32602 Invalid params, got %+v", errObj)
	}
}

func TestAudit_Ping(t *testing.T) {
	srv, _ := auditServer(t)
	_, env := auditRPC(srv, 1, "ping", map[string]any{}, nil)
	if env["error"] != nil {
		t.Fatalf("ping errored: %+v", env["error"])
	}
	if _, ok := env["result"].(map[string]any); !ok {
		t.Fatalf("ping result must be object: %+v", env["result"])
	}
}

func TestAudit_Auth_BearerCaseInsensitive(t *testing.T) {
	srv, _ := auditServer(t)
	for _, prefix := range []string{"Bearer ", "bearer ", "BEARER ", "BeArEr "} {
		body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
		w := auditDo(srv, http.MethodPost, "/mcp", prefix+"tok", body, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("prefix %q: code=%d body=%s", prefix, w.Code, w.Body.String())
		}
	}
}

func TestAudit_Auth_Missing_401_WWWAuthenticate(t *testing.T) {
	srv, _ := auditServer(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	w := auditDo(srv, http.MethodPost, "/mcp", "", body, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
	ch := w.Header().Get("WWW-Authenticate")
	if !strings.HasPrefix(strings.ToLower(ch), "bearer") {
		t.Fatalf("WWW-Authenticate missing bearer: %q", ch)
	}
	if !strings.Contains(ch, `resource_metadata=`) {
		t.Fatalf("WWW-Authenticate must include resource_metadata: %q", ch)
	}
	if !strings.Contains(ch, "/.well-known/oauth-protected-resource") {
		t.Fatalf("resource_metadata should point at /.well-known/oauth-protected-resource: %q", ch)
	}
}

func TestAudit_Auth_InvalidBearer_401(t *testing.T) {
	v, _ := vault.New(bytes.Repeat([]byte{1}, 32), 1)
	cat := ucmcp.NewStaticCatalog()
	bindings := memrepo.NewBuiltinBindingRepo()
	r := ucmcp.NewRegistry(cat, bindings, memrepo.NewRemoteServerRepo(), memrepo.NewToolCacheRepo(), v)
	uc := ucmcp.NewCallToolUseCase(r)
	srv := New(r, uc, stubResolver{err: ErrUnauthorized})
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	w := auditDo(srv, http.MethodPost, "/mcp", "Bearer bad", body, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestAudit_Auth_MethodNotAllowed_HasWWWAuthenticate(t *testing.T) {
	srv, _ := auditServer(t)
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", w.Code)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("WWW-Authenticate required on 405 for discovery")
	}
}

func TestAudit_WellKnown_ProtectedResource(t *testing.T) {
	_, meta := auditServer(t)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	w := httptest.NewRecorder()
	meta.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("want application/json, got %q", ct)
	}
	var doc map[string]any
	body, _ := io.ReadAll(w.Body)
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"resource", "authorization_servers"} {
		if _, ok := doc[k]; !ok {
			t.Fatalf("well-known doc missing %q: %+v", k, doc)
		}
	}

	w2 := httptest.NewRecorder()
	meta.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/.well-known/oauth-protected-resource", nil))
	if w2.Code != http.StatusMethodNotAllowed {
		t.Fatalf("non-GET: want 405, got %d", w2.Code)
	}
}

func TestAudit_Origin_Rebinding(t *testing.T) {
	srv, _ := auditServer(t)
	srv.AllowedOrigins = []string{"https://app.vozko.test"}
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	t.Run("allowed", func(t *testing.T) {
		w := auditDo(srv, http.MethodPost, "/mcp", "Bearer t", body,
			map[string]string{"Origin": "https://app.vozko.test"})
		if w.Code != http.StatusOK {
			t.Fatalf("allowed origin got %d", w.Code)
		}
	})
	t.Run("blocked", func(t *testing.T) {
		w := auditDo(srv, http.MethodPost, "/mcp", "Bearer t", body,
			map[string]string{"Origin": "https://evil.test"})
		if w.Code != http.StatusForbidden {
			t.Fatalf("blocked origin got %d, want 403", w.Code)
		}
	})
	t.Run("no-origin-bypass", func(t *testing.T) {

		w := auditDo(srv, http.MethodPost, "/mcp", "Bearer t", body, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("no-origin got %d", w.Code)
		}
	})
}

func TestAudit_ExtractBearer(t *testing.T) {
	cases := map[string]string{
		"Bearer tok":   "tok",
		"bearer tok":   "tok",
		"BEARER  tok ": "tok",
		"":             "",
		"Basic xyz":    "",
		"Bear":         "",
	}
	for in, want := range cases {
		if got := extractBearer(in); got != want {
			t.Fatalf("extractBearer(%q) = %q want %q", in, got, want)
		}
	}
}
