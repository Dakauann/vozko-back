package mcp

import (
	"context"
	"errors"
	"testing"

	domainmcp "vozko/domain/agent/mcp"
	memrepo "vozko/infra/repositories/agent/mcp"
)

type fakeProber struct {
	tools []domainmcp.Tool
	err   error
	call  int
}

func (f *fakeProber) Probe(_ context.Context, _ string, _ ProbeAuth) ([]domainmcp.Tool, error) {
	f.call++
	return f.tools, f.err
}

func TestRegisterRemoteAPIKeyHappy(t *testing.T) {
	v := newVault(t)
	remotes := memrepo.NewRemoteServerRepo()
	cache := memrepo.NewToolCacheRepo()
	pr := &fakeProber{tools: []domainmcp.Tool{{Name: "t", InputSchema: []byte(`{}`)}}}
	uc := RegisterRemoteUseCase{Remotes: remotes, Cache: cache, Vault: v, Prober: pr}
	out, err := uc.Execute(context.Background(), RegisterRemoteInput{
		ID: "rid", WorkspaceID: "ws", Name: "n", URL: "https://x.com/mcp", AuthMode: domainmcp.AuthAPIKey, APIKey: "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := out.Server
	if s.Status != domainmcp.StatusConnected {
		t.Fatalf("status=%s", s.Status)
	}
	if s.LastListedAt == nil {
		t.Fatal("last listed nil")
	}
	cached, _ := cache.List(context.Background(), "remote:rid", "ws")
	if len(cached) != 1 {
		t.Fatalf("cached=%+v", cached)
	}
}

func TestRegisterRemoteAuthNone(t *testing.T) {
	v := newVault(t)
	uc := RegisterRemoteUseCase{Remotes: memrepo.NewRemoteServerRepo(), Cache: memrepo.NewToolCacheRepo(), Vault: v, Prober: &fakeProber{}}
	if _, err := uc.Execute(context.Background(), RegisterRemoteInput{ID: "r", WorkspaceID: "ws", Name: "n", URL: "https://x.com/mcp", AuthMode: domainmcp.AuthNone}); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterRemoteOAuth2RequiresDiscoverer(t *testing.T) {

	uc := RegisterRemoteUseCase{Remotes: memrepo.NewRemoteServerRepo(), Cache: memrepo.NewToolCacheRepo(), Vault: newVault(t), Prober: &fakeProber{}}
	if _, err := uc.Execute(context.Background(), RegisterRemoteInput{ID: "r", WorkspaceID: "ws", Name: "n", URL: "https://x.com/mcp", AuthMode: domainmcp.AuthOAuth2}); err == nil {
		t.Fatal()
	}
}

func TestRegisterRemoteValidation(t *testing.T) {
	uc := RegisterRemoteUseCase{Remotes: memrepo.NewRemoteServerRepo(), Cache: memrepo.NewToolCacheRepo(), Vault: newVault(t), Prober: &fakeProber{}}
	if _, err := uc.Execute(context.Background(), RegisterRemoteInput{AuthMode: "bad"}); !errors.Is(err, domainmcp.ErrUnknownAuthMode) {
		t.Fatal(err)
	}
	if _, err := uc.Execute(context.Background(), RegisterRemoteInput{AuthMode: domainmcp.AuthNone, WorkspaceID: "ws", Name: "n", URL: "http://x"}); !errors.Is(err, domainmcp.ErrURLNotHTTPS) {
		t.Fatal(err)
	}
	if _, err := uc.Execute(context.Background(), RegisterRemoteInput{AuthMode: domainmcp.AuthAPIKey, WorkspaceID: "ws", Name: "n", URL: "https://x.com/mcp"}); !errors.Is(err, domainmcp.ErrCredentialRequired) {
		t.Fatal(err)
	}
}

func TestRegisterRemoteProbeFailsPersistsError(t *testing.T) {
	v := newVault(t)
	remotes := memrepo.NewRemoteServerRepo()
	pr := &fakeProber{err: errors.New("net")}
	uc := RegisterRemoteUseCase{Remotes: remotes, Cache: memrepo.NewToolCacheRepo(), Vault: v, Prober: pr}
	_, err := uc.Execute(context.Background(), RegisterRemoteInput{ID: "rid", WorkspaceID: "ws", Name: "n", URL: "https://x.com/mcp", AuthMode: domainmcp.AuthNone})
	if err == nil {
		t.Fatal()
	}
	got, _ := remotes.Get(context.Background(), "ws", "rid")
	if got == nil || got.Status != domainmcp.StatusError {
		t.Fatalf("%+v", got)
	}
}

func TestClientProberHandshakeFails(t *testing.T) {

	pr := ClientProber{}
	if _, err := pr.Probe(context.Background(), "http://127.0.0.1:1/", ProbeAuth{APIKey: "k"}); err == nil {
		t.Fatal("expected error")
	}
}
