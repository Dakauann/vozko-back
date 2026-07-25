package mcp

import (
	"context"
	"errors"
	"testing"

	domainmcp "vozko/domain/agent/mcp"
	memrepo "vozko/infra/repositories/agent/mcp"
)

func TestConfigureBuiltinAPIKey(t *testing.T) {
	v := newVault(t)
	cat := NewStaticCatalog(domainmcp.BuiltinDescriptor{Key: "k", AuthSpec: domainmcp.BuiltinAuthSpec{Mode: domainmcp.AuthAPIKey}})
	bindings := memrepo.NewBuiltinBindingRepo()
	_ = bindings.Upsert(context.Background(), &domainmcp.BuiltinBinding{ID: "1", WorkspaceID: "ws", ServerKey: "k", Status: domainmcp.StatusPending})
	uc := ConfigureBuiltinAuthUseCase{Catalog: cat, Bindings: bindings, Vault: v}

	b, err := uc.ExecuteAPIKey(context.Background(), ConfigureBuiltinAPIKeyInput{WorkspaceID: "ws", BindingID: "1", APIKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != domainmcp.StatusConnected {
		t.Fatal()
	}
	plain, err := v.Open(b.Credential.Cipher)
	if err != nil || string(plain) != "secret" {
		t.Fatalf("plain=%q err=%v", plain, err)
	}
}

func TestConfigureBuiltinErrors(t *testing.T) {
	v := newVault(t)
	cat := NewStaticCatalog(
		domainmcp.BuiltinDescriptor{Key: "apikey", AuthSpec: domainmcp.BuiltinAuthSpec{Mode: domainmcp.AuthAPIKey}},
		domainmcp.BuiltinDescriptor{Key: "none", AuthSpec: domainmcp.BuiltinAuthSpec{Mode: domainmcp.AuthNone}},
	)
	bindings := memrepo.NewBuiltinBindingRepo()
	_ = bindings.Upsert(context.Background(), &domainmcp.BuiltinBinding{ID: "apikey-b", WorkspaceID: "ws", ServerKey: "apikey", Status: domainmcp.StatusPending})
	_ = bindings.Upsert(context.Background(), &domainmcp.BuiltinBinding{ID: "none-b", WorkspaceID: "ws", ServerKey: "none", Status: domainmcp.StatusPending})
	uc := ConfigureBuiltinAuthUseCase{Catalog: cat, Bindings: bindings, Vault: v}

	if _, err := uc.ExecuteAPIKey(context.Background(), ConfigureBuiltinAPIKeyInput{WorkspaceID: "ws", BindingID: "apikey-b"}); !errors.Is(err, domainmcp.ErrCredentialRequired) {
		t.Fatal(err)
	}

	if _, err := uc.ExecuteAPIKey(context.Background(), ConfigureBuiltinAPIKeyInput{WorkspaceID: "ws", BindingID: "missing", APIKey: "x"}); !errors.Is(err, domainmcp.ErrBindingNotFound) {
		t.Fatal(err)
	}

	if _, err := uc.ExecuteAPIKey(context.Background(), ConfigureBuiltinAPIKeyInput{WorkspaceID: "ws", BindingID: "none-b", APIKey: "x"}); !errors.Is(err, domainmcp.ErrUnknownAuthMode) {
		t.Fatal(err)
	}
}
