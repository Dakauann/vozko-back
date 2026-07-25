package mcp

import (
	"context"
	"errors"
	"testing"

	domainmcp "vozko/domain/agent/mcp"
)

func TestRegisterRemoteRepoCreateError(t *testing.T) {
	v := newVault(t)
	uc := RegisterRemoteUseCase{
		Remotes: failingRemotes{err: errors.New("repo")},
		Cache:   failingCache{},
		Vault:   v,
		Prober:  &fakeProber{tools: []domainmcp.Tool{{Name: "t", InputSchema: []byte(`{}`)}}},
	}
	if _, err := uc.Execute(context.Background(), RegisterRemoteInput{
		ID: "rid", WorkspaceID: "ws", Name: "n", URL: "https://x.com/mcp", AuthMode: domainmcp.AuthNone,
	}); err == nil {
		t.Fatal()
	}
}

func TestRegisterRemoteCacheError(t *testing.T) {
	v := newVault(t)
	uc := RegisterRemoteUseCase{
		Remotes: stubRemotes{},
		Cache:   failingCache{err: errors.New("cache")},
		Vault:   v,
		Prober:  &fakeProber{tools: []domainmcp.Tool{{Name: "t", InputSchema: []byte(`{}`)}}},
	}
	if _, err := uc.Execute(context.Background(), RegisterRemoteInput{
		ID: "rid", WorkspaceID: "ws", Name: "n", URL: "https://x.com/mcp", AuthMode: domainmcp.AuthNone,
	}); err == nil {
		t.Fatal()
	}
}

func TestEnableBuiltinUpsertError(t *testing.T) {
	cat := NewStaticCatalog(domainmcp.BuiltinDescriptor{Key: "k"})
	uc := EnableBuiltinUseCase{Catalog: cat, Bindings: failingBindings{err: errors.New("up")}}
	if _, err := uc.Execute(context.Background(), EnableBuiltinInput{WorkspaceID: "ws", ServerKey: "k"}); err == nil {
		t.Fatal()
	}
}

func TestConfigureBuiltinUpsertError(t *testing.T) {
	v := newVault(t)
	cat := NewStaticCatalog(domainmcp.BuiltinDescriptor{Key: "k", AuthSpec: domainmcp.BuiltinAuthSpec{Mode: domainmcp.AuthAPIKey}})
	uc := ConfigureBuiltinAuthUseCase{Catalog: cat, Bindings: stubBindingsGet{
		ret: &domainmcp.BuiltinBinding{ID: "1", WorkspaceID: "ws", ServerKey: "k"},
		up:  errors.New("upsert"),
	}, Vault: v}
	if _, err := uc.ExecuteAPIKey(context.Background(), ConfigureBuiltinAPIKeyInput{WorkspaceID: "ws", BindingID: "1", APIKey: "x"}); err == nil {
		t.Fatal()
	}
}

type stubBindingsGet struct {
	ret *domainmcp.BuiltinBinding
	up  error
}

func (s stubBindingsGet) Upsert(context.Context, *domainmcp.BuiltinBinding) error { return s.up }
func (s stubBindingsGet) GetByID(context.Context, string, string) (*domainmcp.BuiltinBinding, error) {
	return s.ret, nil
}
func (s stubBindingsGet) ListByWorkspace(context.Context, string) ([]*domainmcp.BuiltinBinding, error) {
	return nil, nil
}
func (s stubBindingsGet) Delete(context.Context, string, string) error { return nil }
