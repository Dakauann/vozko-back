package unofficial_whatsapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

// EnsurePlatformServerUseCase seeds the platform host from configuration.
//
// Idempotent by base URL, because it runs on every boot and every replica: a
// second row for the same host would split its capacity accounting in two and
// let placement overfill it.
type EnsurePlatformServerUseCase struct {
	servers uw.ServerRepository
}

func NewEnsurePlatformServerUseCase(servers uw.ServerRepository) *EnsurePlatformServerUseCase {
	return &EnsurePlatformServerUseCase{servers: servers}
}

// PlatformServerInput is the configured host.
type PlatformServerInput struct {
	Name       string
	BaseURL    string
	AdminToken string
	Capacity   int
}

func (uc *EnsurePlatformServerUseCase) Execute(ctx context.Context, in PlatformServerInput) (*uw.Server, error) {
	server := &uw.Server{
		Name:       in.Name,
		BaseURL:    in.BaseURL,
		AdminToken: in.AdminToken,
		Capacity:   in.Capacity,
		Enabled:    true,
	}
	server.Normalize()
	if err := server.Validate(); err != nil {
		return nil, err
	}

	existing, err := uc.servers.FindByBaseURL(ctx, server.BaseURL)
	switch {
	case err == nil:
		// Config is the source of truth for credentials and capacity, so a
		// rotated admin token or a resized host takes effect on the next boot
		// without a manual step. InUse is left alone: it is owned by placement.
		existing.Name = server.Name
		existing.AdminToken = server.AdminToken
		existing.Capacity = server.Capacity
		existing.Enabled = true
		if err := uc.servers.Update(ctx, existing); err != nil {
			return nil, err
		}
		return existing, nil
	case errors.Is(err, uw.ErrServerNotFound):
		if err := uc.servers.Create(ctx, server); err != nil {
			return nil, err
		}
		return server, nil
	default:
		return nil, err
	}
}

// ReconcileServerCapacityUseCase realigns our capacity counters with the hosts.
//
// Two things drift and both are invisible until they bite: our InUse counter
// (a crash between claiming a slot and persisting an instance leaks one), and
// the hosts themselves (an instance created but never persisted holds a slot
// forever). A counter that drifts silently is worse than no counter, because
// placement trusts it.
type ReconcileServerCapacityUseCase struct {
	servers   uw.ServerRepository
	instances uw.InstanceRepository
	provider  uw.ProviderAPI
}

func NewReconcileServerCapacityUseCase(
	servers uw.ServerRepository,
	instances uw.InstanceRepository,
	provider uw.ProviderAPI,
) *ReconcileServerCapacityUseCase {
	return &ReconcileServerCapacityUseCase{servers: servers, instances: instances, provider: provider}
}

func (uc *ReconcileServerCapacityUseCase) Execute(ctx context.Context) error {
	servers, err := uc.servers.ListAll(ctx)
	if err != nil {
		return err
	}
	for _, server := range servers {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// One host's failure never stops the sweep: the others still need their
		// counters corrected.
		if err := uc.reconcile(ctx, server); err != nil {
			log.Printf("[unofficial-whatsapp] server %s reconciliation failed: %v", server.ID, err)
			_ = uc.servers.RecordHealth(ctx, server.ID, nil, err.Error())
		}
	}
	return nil
}

func (uc *ReconcileServerCapacityUseCase) reconcile(ctx context.Context, server *uw.Server) error {
	// Our own count is authoritative for capacity: a slot is consumed by an
	// instance we can address, and one we cannot is a leak to report, not a
	// slot to keep reserving.
	ours, err := uc.instances.CountByServer(ctx, server.ID)
	if err != nil {
		return fmt.Errorf("count local instances: %w", err)
	}
	if err := uc.servers.SyncCapacity(ctx, server.ID, ours); err != nil {
		return fmt.Errorf("sync capacity: %w", err)
	}

	remote, err := uc.provider.ListInstances(ctx, uw.ServerRef{
		BaseURL:    server.BaseURL,
		AdminToken: server.AdminToken,
	})
	if err != nil {
		return fmt.Errorf("list host instances: %w", err)
	}

	uc.reportOrphans(ctx, server, remote)

	now := time.Now().UTC()
	return uc.servers.RecordHealth(ctx, server.ID, &now, "")
}

// reportOrphans names instances the host holds that we cannot address.
//
// They are reported rather than deleted: an automated delete against a host we
// share with nothing else would be safe, but a misconfigured base URL pointing
// two environments at one host would make it destructive. The admin metadata we
// wrote at provisioning is what makes them identifiable in the first place.
func (uc *ReconcileServerCapacityUseCase) reportOrphans(ctx context.Context, server *uw.Server, remote []uw.RemoteInstance) {
	for _, item := range remote {
		if strings.TrimSpace(item.OurInstanceID) == "" {
			// Not ours to reason about: another system may share this host.
			continue
		}
		_, err := uc.instances.FindByProviderInstanceID(ctx, server.ID, item.ProviderInstanceID)
		if err == nil {
			continue
		}
		if !errors.Is(err, uw.ErrInstanceNotFound) {
			log.Printf("[unofficial-whatsapp] server %s: orphan check failed for %s: %v",
				server.ID, item.ProviderInstanceID, err)
			continue
		}
		log.Printf("[unofficial-whatsapp] server %s: ORPHAN instance %s (ours=%s, workspace=%s) holds a slot on the host",
			server.ID, item.ProviderInstanceID, item.OurInstanceID, item.WorkspaceID)
	}
}
