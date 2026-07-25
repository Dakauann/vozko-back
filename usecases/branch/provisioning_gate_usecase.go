package branch_usecase

import (
	"vozko/domain/branch"
	workspace_addon "vozko/domain/workspace/workspace_addon"
)

// branchProvisioningGate enforces the per-workspace branch cap from the plan
// entitlement, mirroring the WhatsApp phoneProvisioningGate. The limit is
// PlanDefinition.MaxBranches plus active addons (EntitlementBranches); a
// workspace with no active subscription or a 0 entitlement cannot create one.
type branchProvisioningGate struct {
	entitlements workspace_addon.EntitlementResolver
	repo         branch.Repository
}

func NewProvisioningGate(entitlements workspace_addon.EntitlementResolver, repo branch.Repository) branch.ProvisioningGate {
	return &branchProvisioningGate{entitlements: entitlements, repo: repo}
}

func (g *branchProvisioningGate) CanCreateBranch(workspaceID string) (bool, error) {
	limit, err := g.entitlements.Resolve(workspaceID, workspace_addon.EntitlementBranches)
	if err != nil {
		return false, err
	}
	if limit <= 0 {
		return false, nil
	}
	count, err := g.repo.CountByWorkspace(workspaceID)
	if err != nil {
		return false, err
	}
	return count < int64(limit), nil
}
