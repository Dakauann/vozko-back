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

type errSrc struct{}

func (errSrc) ID() string           { return "builtin:k" }
func (errSrc) Kind() domainmcp.Kind { return domainmcp.KindBuiltin }
func (errSrc) DisplayName() string  { return "" }
func (errSrc) ListTools(context.Context, domainmcp.WorkspaceID) ([]domainmcp.Tool, error) {
	return nil, errors.New("nope")
}
func (errSrc) CallTool(context.Context, domainmcp.WorkspaceID, string, map[string]any) (domainmcp.ToolResult, error) {
	return domainmcp.ToolResult{}, nil
}

func TestPublicServerListErrFromSource(t *testing.T) {
	v, _ := vault.New(bytes.Repeat([]byte{1}, 32), 1)
	cat := ucmcp.NewStaticCatalog(domainmcp.BuiltinDescriptor{Key: "k", Builder: func(*domainmcp.Credential) domainmcp.ToolSource { return errSrc{} }})
	bindings := memrepo.NewBuiltinBindingRepo()
	_ = bindings.Upsert(context.Background(), &domainmcp.BuiltinBinding{ID: "1", WorkspaceID: "ws", ServerKey: "k", Status: domainmcp.StatusConnected})
	r := ucmcp.NewRegistry(cat, bindings, memrepo.NewRemoteServerRepo(), memrepo.NewToolCacheRepo(), v)
	srv := New(r, ucmcp.NewCallToolUseCase(r), stubResolver{ws: "ws"})
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{}})
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "nope") {
		t.Fatalf("body=%s", w.Body.String())
	}
}

type failingBindingsList struct{}

func (failingBindingsList) Upsert(context.Context, *domainmcp.BuiltinBinding) error { return nil }
func (failingBindingsList) GetByID(context.Context, string, string) (*domainmcp.BuiltinBinding, error) {
	return nil, nil
}
func (failingBindingsList) ListByWorkspace(context.Context, string) ([]*domainmcp.BuiltinBinding, error) {
	return nil, errors.New("repofail")
}
func (failingBindingsList) Delete(context.Context, string, string) error { return nil }

func TestPublicServerListErrFromRegistry(t *testing.T) {
	v, _ := vault.New(bytes.Repeat([]byte{1}, 32), 1)
	r := ucmcp.NewRegistry(ucmcp.NewStaticCatalog(), failingBindingsList{}, memrepo.NewRemoteServerRepo(), memrepo.NewToolCacheRepo(), v)
	srv := New(r, ucmcp.NewCallToolUseCase(r), stubResolver{ws: "ws"})
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{}})
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "repofail") {
		t.Fatalf("body=%s", w.Body.String())
	}
}
