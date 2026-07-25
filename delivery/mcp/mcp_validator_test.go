package mcphttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/agent/mcp/vault"
	memrepo "vozko/infra/repositories/agent/mcp"
	ucmcp "vozko/usecases/agent/mcp"
)

func TestMCPValidator(t *testing.T) {
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

	ts := httptest.NewServer(srv)
	defer ts.Close()

	call := func(t *testing.T, id int, method string, params any) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": id, "method": method, "params": params,
		})
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer valid-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s transport: %v", method, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s http=%d body=%s", method, resp.StatusCode, b)
		}
		var env map[string]any
		if err := json.Unmarshal(b, &env); err != nil {
			t.Fatalf("%s: invalid json envelope: %v: %s", method, err, b)
		}
		if env["jsonrpc"] != "2.0" {
			t.Fatalf("%s: jsonrpc must be \"2.0\" got %v", method, env["jsonrpc"])
		}
		return env
	}

	t.Run("initialize", func(t *testing.T) {
		env := call(t, 1, "initialize", map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "validator", "version": "1.0"},
		})
		if _, bad := env["error"]; bad {
			t.Fatalf("initialize returned error: %+v", env["error"])
		}
		res, ok := env["result"].(map[string]any)
		if !ok {
			t.Fatalf("initialize missing result: %+v", env)
		}
		if res["protocolVersion"] != "2025-06-18" {
			t.Fatalf("bad protocolVersion: %v", res["protocolVersion"])
		}
		if _, ok := res["serverInfo"].(map[string]any); !ok {
			t.Fatalf("initialize missing serverInfo: %+v", res)
		}
		if _, ok := res["capabilities"].(map[string]any); !ok {
			t.Fatalf("initialize missing capabilities: %+v", res)
		}
	})

	t.Run("tools/list", func(t *testing.T) {
		env := call(t, 2, "tools/list", map[string]any{})
		res := env["result"].(map[string]any)
		tools, _ := res["tools"].([]any)
		if len(tools) == 0 {
			t.Fatalf("tools/list: expected at least one tool, got %+v", res)
		}
		for _, rawTool := range tools {
			tl, ok := rawTool.(map[string]any)
			if !ok {
				t.Fatalf("tools/list: non-object tool %T", rawTool)
			}
			if _, ok := tl["name"].(string); !ok || tl["name"] == "" {
				t.Fatalf("tools/list: tool missing name: %+v", tl)
			}
			if _, ok := tl["inputSchema"].(map[string]any); !ok {
				t.Fatalf("tools/list: tool missing inputSchema: %+v", tl)
			}
		}
	})

	t.Run("tools/call", func(t *testing.T) {
		env := call(t, 3, "tools/call", map[string]any{
			"name":      "builtin:echo.say",
			"arguments": map[string]any{"msg": "ping"},
		})
		if _, bad := env["error"]; bad {
			t.Fatalf("tools/call returned error: %+v", env["error"])
		}
		res := env["result"].(map[string]any)
		content, _ := res["content"].([]any)
		if len(content) == 0 {
			t.Fatalf("tools/call: empty content: %+v", res)
		}
		first := content[0].(map[string]any)
		if first["type"] != "text" {
			t.Fatalf("tools/call: unexpected content type %v", first["type"])
		}
		if first["text"] != "ping" {
			t.Fatalf("tools/call: echo mismatch: %v", first["text"])
		}
	})

	t.Run("error-envelope", func(t *testing.T) {
		env := call(t, 4, "tools/call", map[string]any{"name": "builtin:echo.nope"})
		errObj, ok := env["error"].(map[string]any)
		if !ok {
			t.Fatalf("expected error object: %+v", env)
		}
		if _, ok := errObj["code"].(float64); !ok {
			t.Fatalf("error.code must be number: %+v", errObj)
		}
		if _, ok := errObj["message"].(string); !ok {
			t.Fatalf("error.message must be string: %+v", errObj)
		}
	})

	t.Run("unknown-method", func(t *testing.T) {
		env := call(t, 5, "does/not/exist", map[string]any{})
		errObj, ok := env["error"].(map[string]any)
		if !ok {
			t.Fatalf("expected error object for unknown method: %+v", env)
		}
		code := errObj["code"].(float64)
		if code == 0 {
			t.Fatalf("unknown method: expected non-zero error code, got 0")
		}
	})
}
