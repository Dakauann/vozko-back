package mcp

import (
	"context"
	"errors"
	"testing"

	domainmcp "vozko/domain/agent/mcp"
	memrepo "vozko/infra/repositories/agent/mcp"
)

func TestEnableBuiltinAuthNone(t *testing.T) {
	cat := NewStaticCatalog(domainmcp.BuiltinDescriptor{Key: "k", DisplayName: "K", AuthSpec: domainmcp.BuiltinAuthSpec{Mode: domainmcp.AuthNone}})
	bindings := memrepo.NewBuiltinBindingRepo()
	uc := EnableBuiltinUseCase{Catalog: cat, Bindings: bindings}
	b, err := uc.Execute(context.Background(), EnableBuiltinInput{WorkspaceID: "ws", ServerKey: "k", BindingID: "id"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != domainmcp.StatusConnected {
		t.Fatal()
	}
}

func TestEnableBuiltinAuthAPIKey(t *testing.T) {
	cat := NewStaticCatalog(domainmcp.BuiltinDescriptor{Key: "k", AuthSpec: domainmcp.BuiltinAuthSpec{Mode: domainmcp.AuthAPIKey}})
	bindings := memrepo.NewBuiltinBindingRepo()
	uc := EnableBuiltinUseCase{Catalog: cat, Bindings: bindings}
	b, err := uc.Execute(context.Background(), EnableBuiltinInput{WorkspaceID: "ws", ServerKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != domainmcp.StatusPending {
		t.Fatal()
	}
}

func TestEnableBuiltinUnknown(t *testing.T) {
	uc := EnableBuiltinUseCase{Catalog: NewStaticCatalog(), Bindings: memrepo.NewBuiltinBindingRepo()}
	if _, err := uc.Execute(context.Background(), EnableBuiltinInput{WorkspaceID: "ws", ServerKey: "x"}); !errors.Is(err, domainmcp.ErrServerKeyRequired) {
		t.Fatal(err)
	}
}

func TestEnableBuiltinValidation(t *testing.T) {
	cat := NewStaticCatalog(domainmcp.BuiltinDescriptor{Key: "k"})
	uc := EnableBuiltinUseCase{Catalog: cat, Bindings: memrepo.NewBuiltinBindingRepo()}
	if _, err := uc.Execute(context.Background(), EnableBuiltinInput{ServerKey: "k"}); !errors.Is(err, domainmcp.ErrWorkspaceRequired) {
		t.Fatal(err)
	}
}
