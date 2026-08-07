package unofficial_whatsapp

import (
	"context"

	uw "vozko/domain/unofficial_whatsapp"
	workspace_addon "vozko/domain/workspace/workspace_addon"
)

// The adapter between the platform's entitlement stack and this channel.
//
// It exists so the channel depends on `InstanceAllowance` — a limit and a
// usage — and never on how that limit is composed. Today the base comes from
// per-workspace configuration and the top-up from active addons, which is
// already different from every other entitlement kind; tomorrow it could come
// from a plan field, and nothing in the channel would change.
//
// The direction of the dependency is the point: billing knows nothing about
// WhatsApp, and WhatsApp knows nothing about plans, addons or subscriptions.

// WorkspaceEntitlementReader is the narrow slice of the addon stack this needs.
// Satisfied by workspace_addon.GetWorkspaceEntitlementsUseCase.
type WorkspaceEntitlementReader interface {
	Execute(workspaceID string) ([]workspace_addon.WorkspaceEntitlement, error)
}

// InstanceEntitlementReader is exported because the container holds the concrete
// type: its source is attached after construction, and the boot log asserts that
// it was.
type InstanceEntitlementReader struct {
	entitlements WorkspaceEntitlementReader
	instances    uw.InstanceRepository
}

// NewInstanceEntitlementReader builds the channel's allowance reader.
//
// It returns a POINTER, not the interface, and the entitlement source is
// attached afterwards through SetSource. That is not ceremony: the channel is
// built before the billing use cases exist — the container's own comment says
// so, because the handlers must be ready for the router — so taking the source
// as a constructor argument would capture a nil and every workspace would
// resolve to zero numbers. Handing out the pointer lets the provisioning gate
// and the HTTP handler hold this object early and still see the source once it
// is wired.
func NewInstanceEntitlementReader(instances uw.InstanceRepository) *InstanceEntitlementReader {
	return &InstanceEntitlementReader{instances: instances}
}

// SetSource attaches the platform's entitlement stack, once it exists.
//
// Until it is called the reader answers zero, which fails provisioning CLOSED
// rather than open. That is the right way round — an unwired gate must not hand
// out capacity — but it is still a wiring fault, so the container asserts it at
// boot instead of leaving it to be discovered from a customer's complaint.
func (r *InstanceEntitlementReader) SetSource(entitlements WorkspaceEntitlementReader) {
	r.entitlements = entitlements
}

// HasSource reports whether the entitlement stack is attached. Read by the
// container's boot-time capability log.
func (r *InstanceEntitlementReader) HasSource() bool { return r != nil && r.entitlements != nil }

// AllowanceFor reports what this workspace may hold and what it already holds.
//
// Both halves are read here rather than by the caller, because a limit without
// its usage is not an answer to any question the product asks — the gate needs
// the comparison and the UI needs the counter, and computing usage in two places
// is how "3 of 5" and "you cannot connect another" start disagreeing.
func (r *InstanceEntitlementReader) AllowanceFor(
	ctx context.Context,
	workspaceID string,
) (uw.InstanceAllowance, error) {
	allowance, err := r.limitFor(workspaceID)
	if err != nil {
		return uw.InstanceAllowance{}, err
	}
	used, err := r.instances.CountByWorkspace(ctx, workspaceID)
	if err != nil {
		return uw.InstanceAllowance{}, err
	}
	allowance.Used = used
	return allowance, nil
}

// limitFor extracts this channel's kind from the workspace's entitlements.
//
// A kind that is absent from the list resolves to ZERO rather than to an error.
// That is the correct reading: the entitlement stack returns an entry for every
// kind it knows, so an absent one means a workspace granted nothing — not a
// failure. Erroring would turn "you have not been given any numbers yet", the
// normal state of every workspace on day one, into a 500.
func (r *InstanceEntitlementReader) limitFor(workspaceID string) (uw.InstanceAllowance, error) {
	if r.entitlements == nil {
		return uw.InstanceAllowance{}, nil
	}
	ents, err := r.entitlements.Execute(workspaceID)
	if err != nil {
		return uw.InstanceAllowance{}, err
	}
	for _, e := range ents {
		if e.Kind == workspace_addon.EntitlementUnofficialWhatsAppInstances {
			// PlanBase is the workspace-config grant for this kind — see the
			// entitlement's own comment for why it is not a plan field. Carried
			// through separately from the purchased units so the meter can name
			// which half is which.
			return uw.InstanceAllowance{
				Limit:     e.Total,
				Granted:   e.PlanBase,
				Purchased: e.AddonUnits,
			}, nil
		}
	}
	return uw.InstanceAllowance{}, nil
}
