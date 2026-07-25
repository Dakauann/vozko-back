package mcp

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/agent/mcp/vault"
	memrepo "vozko/infra/repositories/agent/mcp"
)

func newVault(t *testing.T) *vault.Vault {
	t.Helper()
	v, err := vault.New(bytes.Repeat([]byte{1}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

type fakeSource struct {
	id    string
	kind  domainmcp.Kind
	name  string
	tools []domainmcp.Tool
	calls map[string]domainmcp.ToolResult
	err   error
}

func (f *fakeSource) ID() string           { return f.id }
func (f *fakeSource) Kind() domainmcp.Kind { return f.kind }
func (f *fakeSource) DisplayName() string  { return f.name }
func (f *fakeSource) ListTools(_ context.Context, _ domainmcp.WorkspaceID) ([]domainmcp.Tool, error) {
	return f.tools, f.err
}
func (f *fakeSource) CallTool(_ context.Context, _ domainmcp.WorkspaceID, n string, _ map[string]any) (domainmcp.ToolResult, error) {
	if f.err != nil {
		return domainmcp.ToolResult{}, f.err
	}
	return f.calls[n], nil
}

type fakeBuilder struct {
	build func(server *domainmcp.RemoteMCPServer, secret string) domainmcp.ToolSource
}

func (f fakeBuilder) Build(s *domainmcp.RemoteMCPServer, sec string) domainmcp.ToolSource {
	return f.build(s, sec)
}

func TestStaticCatalog(t *testing.T) {
	c := NewStaticCatalog(
		domainmcp.BuiltinDescriptor{Key: "b", DisplayName: "B"},
		domainmcp.BuiltinDescriptor{Key: "a", DisplayName: "A"},
	)
	all := c.All()
	if all[0].Key != "a" || all[1].Key != "b" {
		t.Fatalf("not sorted: %+v", all)
	}
	if d, ok := c.Descriptor("a"); !ok || d.DisplayName != "A" {
		t.Fatal()
	}
	if _, ok := c.Descriptor("missing"); ok {
		t.Fatal()
	}
}

func TestRegistrySources(t *testing.T) {
	ctx := context.Background()
	v := newVault(t)

	bA := &fakeSource{id: "builtin:a", kind: domainmcp.KindBuiltin, name: "A"}
	desc := domainmcp.BuiltinDescriptor{Key: "a", DisplayName: "A", Builder: func(_ *domainmcp.Credential) domainmcp.ToolSource { return bA }}
	cat := NewStaticCatalog(desc)

	bindings := memrepo.NewBuiltinBindingRepo()
	_ = bindings.Upsert(ctx, &domainmcp.BuiltinBinding{ID: "1", WorkspaceID: "ws", ServerKey: "a", Status: domainmcp.StatusConnected})

	remotes := memrepo.NewRemoteServerRepo()
	rs, _ := domainmcp.NewRemoteMCPServer("rid", "ws", "Remote", "https://x.com/mcp", "")
	rs.Status = domainmcp.StatusConnected
	sealed, _ := v.Seal([]byte("k"))
	rs.Credential = &domainmcp.Credential{Mode: domainmcp.AuthAPIKey, Cipher: sealed, KEKVersion: 1}
	_ = remotes.Create(ctx, rs)

	cache := memrepo.NewToolCacheRepo()

	r := NewRegistry(cat, bindings, remotes, cache, v)
	rsFake := &fakeSource{id: "remote:rid", kind: domainmcp.KindRemote, name: "Remote"}
	r.Builder = fakeBuilder{build: func(_ *domainmcp.RemoteMCPServer, sec string) domainmcp.ToolSource {
		if sec != "k" {
			t.Fatalf("secret=%q", sec)
		}
		return rsFake
	}}
	srcs, err := r.Sources(ctx, "ws")
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 2 || srcs[0].ID() != "builtin:a" || srcs[1].ID() != "remote:rid" {
		t.Fatalf("%+v", srcs)
	}

	if _, err := r.Sources(ctx, ""); !errors.Is(err, domainmcp.ErrWorkspaceRequired) {
		t.Fatal(err)
	}
}

func TestRegistryResolve(t *testing.T) {
	ctx := context.Background()
	v := newVault(t)
	bs := &fakeSource{id: "builtin:notion", kind: domainmcp.KindBuiltin}
	cat := NewStaticCatalog(domainmcp.BuiltinDescriptor{Key: "notion", Builder: func(*domainmcp.Credential) domainmcp.ToolSource { return bs }})
	bindings := memrepo.NewBuiltinBindingRepo()
	_ = bindings.Upsert(ctx, &domainmcp.BuiltinBinding{ID: "1", WorkspaceID: "ws", ServerKey: "notion", Status: domainmcp.StatusConnected})
	r := NewRegistry(cat, bindings, memrepo.NewRemoteServerRepo(), memrepo.NewToolCacheRepo(), v)

	src, tool, err := r.Resolve(ctx, "ws", "builtin:notion.search")
	if err != nil {
		t.Fatal(err)
	}
	if src != bs || tool != "search" {
		t.Fatalf("%v %s", src, tool)
	}

	if _, _, err := r.Resolve(ctx, "ws", "bad"); err == nil {
		t.Fatal("malformed must fail")
	}
	if _, _, err := r.Resolve(ctx, "ws", "remote:other.x"); !errors.Is(err, domainmcp.ErrToolNotFound) {
		t.Fatal(err)
	}
	if _, _, err := r.Resolve(ctx, "", "builtin:notion.search"); !errors.Is(err, domainmcp.ErrWorkspaceRequired) {
		t.Fatal(err)
	}
}

func TestRegistryDecryptError(t *testing.T) {
	ctx := context.Background()
	v := newVault(t)
	other := newVault(t)

	cat := NewStaticCatalog()
	remotes := memrepo.NewRemoteServerRepo()
	rs, _ := domainmcp.NewRemoteMCPServer("rid", "ws", "R", "https://x.com/mcp", "")
	rs.Status = domainmcp.StatusConnected
	rs.Credential = &domainmcp.Credential{Mode: domainmcp.AuthAPIKey, Cipher: []byte("garbage"), KEKVersion: 1}
	_ = remotes.Create(ctx, rs)
	r := NewRegistry(cat, memrepo.NewBuiltinBindingRepo(), remotes, memrepo.NewToolCacheRepo(), v)
	_ = other
	if _, err := r.Sources(ctx, "ws"); err == nil {
		t.Fatal("expected decrypt error")
	}
}

func TestDefaultSourceBuilder(t *testing.T) {
	rs, _ := domainmcp.NewRemoteMCPServer("rid", "ws", "R", "https://x.com/mcp", "")
	src := DefaultSourceBuilder{}.Build(rs, "")
	if src == nil {
		t.Fatal("nil source")
	}
}

func TestRegistryDecryptNoneAndAuthNone(t *testing.T) {
	v := newVault(t)
	r := NewRegistry(NewStaticCatalog(), memrepo.NewBuiltinBindingRepo(), memrepo.NewRemoteServerRepo(), memrepo.NewToolCacheRepo(), v)
	got, err := r.decryptIfAny(nil)
	if err != nil || got != "" {
		t.Fatal()
	}
	got, err = r.decryptIfAny(&domainmcp.Credential{Mode: domainmcp.AuthNone})
	if err != nil || got != "" {
		t.Fatal()
	}
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry(NewStaticCatalog(), nil, nil, nil, nil)
	if r.Builder == nil {
		t.Fatal("builder default missing")
	}
}

func TestClockSeam(t *testing.T) {
	now := time.Now()
	if now.IsZero() {
		t.Fatal()
	}
}
