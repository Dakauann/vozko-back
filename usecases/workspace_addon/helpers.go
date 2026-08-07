package workspace_addon_usecase

import (
	"context"
	"time"

	wsc "vozko/domain/workspace_config"
)

type clockFn func() time.Time

func utcNow() time.Time { return time.Now().UTC() }

const balanceCurrencyUSD = "USD"

// ---------------------------------------------- per-workspace entitlement base

// includedInstanceReader is the workspace-config half of an entitlement base.
//
// Exactly one kind — unofficial WhatsApp instances — takes its base from
// per-workspace configuration rather than from the plan, because that channel
// has no per-plan pricing and every connected number occupies a slot on a host
// we operate. The interface is narrow and satisfied by the config repository, so
// the addon package does not take a dependency on the whole config domain for
// one integer.
//
// Optional everywhere. A resolver built without one reports ZERO included
// instances, which is the safe answer: no allowance means no provisioning, never
// accidental capacity on hosts we pay for.
type includedInstanceReader interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

// batchIncludedInstanceReader is the same read for many workspaces at once,
// satisfied implicitly by the config repository.
//
// Its own interface so the per-workspace resolvers are not forced to implement a
// batch method they never call, and so a reconciliation sweep over every
// workspace stays a single query rather than one per tenant.
type batchIncludedInstanceReader interface {
	GetIncludedUnofficialInstancesByWorkspaceIDs(ctx context.Context, workspaceIDs []string) (map[string]int, error)
}

// readIncludedInstances resolves one workspace's granted allowance.
//
// A read failure is REPORTED, not swallowed into zero: this number decides
// whether a customer may connect another WhatsApp, and a database blip that
// silently answered "none" would be indistinguishable from an administrator
// having granted them nothing.
func readIncludedInstances(configs includedInstanceReader, workspaceID string) (int, error) {
	if configs == nil {
		return 0, nil
	}
	cfg, err := configs.GetByWorkspaceID(context.Background(), workspaceID)
	if err != nil {
		return 0, err
	}
	if cfg == nil {
		return 0, nil
	}
	return cfg.IncludedUnofficialWhatsAppInstances, nil
}
