package unofficial_whatsapp

import (
	"context"

	uw "vozko/domain/unofficial_whatsapp"
	workspace_addon "vozko/domain/workspace/workspace_addon"
)

type WorkspaceEntitlementReader interface {
	Execute(workspaceID string) ([]workspace_addon.WorkspaceEntitlement, error)
}

type InstanceEntitlementReader struct {
	entitlements WorkspaceEntitlementReader
	instances    uw.InstanceRepository
}

func NewInstanceEntitlementReader(instances uw.InstanceRepository) *InstanceEntitlementReader {
	return &InstanceEntitlementReader{instances: instances}
}

func (r *InstanceEntitlementReader) SetSource(entitlements WorkspaceEntitlementReader) {
	r.entitlements = entitlements
}

func (r *InstanceEntitlementReader) HasSource() bool { return r != nil && r.entitlements != nil }

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
			return uw.InstanceAllowance{
				Limit:     e.Total,
				Granted:   e.PlanBase,
				Purchased: e.AddonUnits,
			}, nil
		}
	}
	return uw.InstanceAllowance{}, nil
}
