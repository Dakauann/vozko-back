package emailsending

import (
	"context"
	"time"
)

type TenantResources struct {
	Name       string
	Identities []string
}

type TenantLifecycle interface {
	RetireTenant(ctx context.Context, tenant TenantResources) error
	ReviveTenant(ctx context.Context, tenant TenantResources) (Tenant, error)
}

type SendActivity interface {
	LastSentAt(ctx context.Context, workspaceID string) (*time.Time, error)
	SentFromSharedDomains(ctx context.Context, workspaceID string) (bool, error)
}
