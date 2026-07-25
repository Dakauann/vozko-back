package branch

// ProvisioningGate answers whether a workspace may create another branch, given
// its plan entitlement (PlanDefinition.MaxBranches plus any active addons,
// resolved through EntitlementBranches) versus its current branch count. This is
// the same plan-driven model MaxCallChannels uses for concurrent calls, not a
// static per-node config value. Implemented in usecases/branch and injected into
// the create use case.
type ProvisioningGate interface {
	CanCreateBranch(workspaceID string) (bool, error)
}
