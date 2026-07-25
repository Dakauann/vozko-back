package mcp

import (
	"context"
	"errors"
	"testing"

	domainmcp "vozko/domain/agent/mcp"
)

type failingBindings struct{ err error }

func (f failingBindings) Upsert(context.Context, *domainmcp.BuiltinBinding) error { return f.err }
func (f failingBindings) GetByID(context.Context, string, string) (*domainmcp.BuiltinBinding, error) {
	return nil, f.err
}
func (f failingBindings) ListByWorkspace(context.Context, string) ([]*domainmcp.BuiltinBinding, error) {
	return nil, f.err
}
func (f failingBindings) Delete(context.Context, string, string) error { return f.err }

type failingRemotes struct{ err error }

func (f failingRemotes) Create(context.Context, *domainmcp.RemoteMCPServer) error { return f.err }
func (f failingRemotes) Update(context.Context, *domainmcp.RemoteMCPServer) error { return f.err }
func (f failingRemotes) Get(context.Context, string, string) (*domainmcp.RemoteMCPServer, error) {
	return nil, f.err
}
func (f failingRemotes) ListByWorkspace(context.Context, string) ([]*domainmcp.RemoteMCPServer, error) {
	return nil, f.err
}
func (f failingRemotes) Delete(context.Context, string, string) error { return f.err }

type failingCache struct{ err error }

func (f failingCache) Replace(context.Context, string, string, []domainmcp.CachedTool) error {
	return f.err
}
func (f failingCache) List(context.Context, string, string) ([]domainmcp.CachedTool, error) {
	return nil, f.err
}
func (f failingCache) Purge(context.Context, string, string) error { return f.err }

func TestRegistrySourcesBindingsErr(t *testing.T) {
	v := newVault(t)
	r := NewRegistry(NewStaticCatalog(), failingBindings{err: errors.New("b")}, failingRemotes{}, failingCache{}, v)
	if _, err := r.Sources(context.Background(), "ws"); err == nil {
		t.Fatal()
	}
}

func TestRegistrySourcesRemotesErr(t *testing.T) {
	v := newVault(t)
	r := NewRegistry(NewStaticCatalog(), failingBindings{}, failingRemotes{err: errors.New("r")}, failingCache{}, v)
	if _, err := r.Sources(context.Background(), "ws"); err == nil {
		t.Fatal()
	}
}

func TestRegistrySourcesMissingDescriptor(t *testing.T) {
	v := newVault(t)
	bindings := failingBindings{}

	r := NewRegistry(NewStaticCatalog(), stubBindings{[]*domainmcp.BuiltinBinding{
		{ID: "1", WorkspaceID: "ws", ServerKey: "missing", Status: domainmcp.StatusConnected},
	}}, failingRemotes{}, failingCache{}, v)
	srcs, err := r.Sources(context.Background(), "ws")
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 0 {
		t.Fatalf("expected empty: %+v", srcs)
	}
	_ = bindings
}

type stubBindings struct{ list []*domainmcp.BuiltinBinding }

func (s stubBindings) Upsert(context.Context, *domainmcp.BuiltinBinding) error { return nil }
func (s stubBindings) GetByID(context.Context, string, string) (*domainmcp.BuiltinBinding, error) {
	return nil, domainmcp.ErrBindingNotFound
}
func (s stubBindings) ListByWorkspace(context.Context, string) ([]*domainmcp.BuiltinBinding, error) {
	return s.list, nil
}
func (s stubBindings) Delete(context.Context, string, string) error { return nil }

func TestRegistrySourcesAuthNoneBuiltin(t *testing.T) {
	v := newVault(t)
	fs := &fakeSource{id: "builtin:k", kind: domainmcp.KindBuiltin}
	cat := NewStaticCatalog(domainmcp.BuiltinDescriptor{Key: "k", AuthSpec: domainmcp.BuiltinAuthSpec{Mode: domainmcp.AuthNone}, Builder: func(c *domainmcp.Credential) domainmcp.ToolSource {
		if c != nil {
			t.Fatal("expected nil cred for auth-none binding without credential")
		}
		return fs
	}})
	r := NewRegistry(cat, stubBindings{[]*domainmcp.BuiltinBinding{
		{ID: "1", WorkspaceID: "ws", ServerKey: "k", Status: domainmcp.StatusConnected},
	}}, failingRemotes{}, failingCache{}, v)
	srcs, err := r.Sources(context.Background(), "ws")
	if err != nil || len(srcs) != 1 {
		t.Fatalf("err=%v srcs=%+v", err, srcs)
	}
}

func TestRegistrySourcesBuiltinDecryptError(t *testing.T) {
	v := newVault(t)
	cat := NewStaticCatalog(domainmcp.BuiltinDescriptor{Key: "k", Builder: func(*domainmcp.Credential) domainmcp.ToolSource { return &fakeSource{id: "builtin:k"} }})
	r := NewRegistry(cat, stubBindings{[]*domainmcp.BuiltinBinding{
		{ID: "1", WorkspaceID: "ws", ServerKey: "k", Status: domainmcp.StatusConnected, Credential: &domainmcp.Credential{Mode: domainmcp.AuthAPIKey, Cipher: []byte("garbage")}},
	}}, failingRemotes{}, failingCache{}, v)
	if _, err := r.Sources(context.Background(), "ws"); err == nil {
		t.Fatal("expected decrypt error")
	}
}

func TestRegistrySourcesPendingRemoteSkipped(t *testing.T) {
	v := newVault(t)
	r := NewRegistry(NewStaticCatalog(), failingBindings{}, stubRemotes{[]*domainmcp.RemoteMCPServer{
		{ID: "a", WorkspaceID: "ws", URL: "https://x", Status: domainmcp.StatusPending},
	}}, failingCache{}, v)
	srcs, err := r.Sources(context.Background(), "ws")
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 0 {
		t.Fatalf("%+v", srcs)
	}
}

type stubRemotes struct{ list []*domainmcp.RemoteMCPServer }

func (s stubRemotes) Create(context.Context, *domainmcp.RemoteMCPServer) error { return nil }
func (s stubRemotes) Update(context.Context, *domainmcp.RemoteMCPServer) error { return nil }
func (s stubRemotes) Get(context.Context, string, string) (*domainmcp.RemoteMCPServer, error) {
	return nil, domainmcp.ErrRemoteServerNotFound
}
func (s stubRemotes) ListByWorkspace(context.Context, string) ([]*domainmcp.RemoteMCPServer, error) {
	return s.list, nil
}
func (s stubRemotes) Delete(context.Context, string, string) error { return nil }
