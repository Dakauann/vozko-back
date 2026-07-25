package mcp

import (
	"context"
	"fmt"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/agent/mcp/vault"
)

type ConfigureBuiltinAPIKeyInput struct {
	WorkspaceID string
	BindingID   string
	APIKey      string
}

type ConfigureBuiltinAuthUseCase struct {
	Catalog  BuiltinCatalog
	Bindings domainmcp.BuiltinBindingRepository
	Vault    *vault.Vault
}

func (u *ConfigureBuiltinAuthUseCase) ExecuteAPIKey(ctx context.Context, in ConfigureBuiltinAPIKeyInput) (*domainmcp.BuiltinBinding, error) {
	if in.APIKey == "" {
		return nil, domainmcp.ErrCredentialRequired
	}
	b, err := u.Bindings.GetByID(ctx, in.WorkspaceID, in.BindingID)
	if err != nil {
		return nil, err
	}
	desc, ok := u.Catalog.Descriptor(b.ServerKey)
	if !ok {
		return nil, fmt.Errorf("%w: %s", domainmcp.ErrServerKeyRequired, b.ServerKey)
	}
	if desc.AuthSpec.Mode != domainmcp.AuthAPIKey {
		return nil, fmt.Errorf("%w: server %s is not api_key", domainmcp.ErrUnknownAuthMode, b.ServerKey)
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
	b.UpdatedAt = domainmcp.Now()
	if err := u.Bindings.Upsert(ctx, b); err != nil {
		return nil, err
	}
	return b, nil
}
