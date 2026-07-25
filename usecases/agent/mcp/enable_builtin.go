package mcp

import (
	"context"
	"fmt"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/agent/mcp/vault"
)

type EnableBuiltinInput struct {
	WorkspaceID string
	ServerKey   string
	BindingID   string
	Label       string
	APIKey      string
}

type EnableBuiltinUseCase struct {
	Catalog  BuiltinCatalog
	Bindings domainmcp.BuiltinBindingRepository
	Vault    *vault.Vault
}

func (u *EnableBuiltinUseCase) Execute(ctx context.Context, in EnableBuiltinInput) (*domainmcp.BuiltinBinding, error) {
	desc, ok := u.Catalog.Descriptor(in.ServerKey)
	if !ok {
		return nil, fmt.Errorf("%w: %s", domainmcp.ErrServerKeyRequired, in.ServerKey)
	}
	b, err := domainmcp.NewBuiltinBinding(in.BindingID, in.WorkspaceID, desc.Key, desc.DisplayName, in.Label)
	if err != nil {
		return nil, err
	}
	switch desc.AuthSpec.Mode {
	case domainmcp.AuthNone:
		b.Status = domainmcp.StatusConnected
	case domainmcp.AuthAPIKey:
		if in.APIKey != "" {
			if u.Vault == nil {
				return nil, fmt.Errorf("mcp: vault not configured")
			}
			sealed, err := u.Vault.Seal([]byte(in.APIKey))
			if err != nil {
				return nil, err
			}
			b.Credential = &domainmcp.Credential{
				Mode:       domainmcp.AuthAPIKey,
				Cipher:     sealed,
				KEKVersion: u.Vault.Version(),
			}
			b.Status = domainmcp.StatusConnected
		}
	}
	if err := u.Bindings.Upsert(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}
