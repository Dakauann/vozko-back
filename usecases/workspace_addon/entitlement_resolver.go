package workspace_addon_usecase

import (
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type entitlementResolver struct {
	subscriptions workspace_plan.CurrentSubscriptionReader
	plans         workspace_plan.PlanReader
	addons        workspace_addon.AddonSubscriptionReader
	configs       includedInstanceReader
	now           clockFn
}

// NewEntitlementResolver builds the per-workspace resolver.
//
// `configs` is a REQUIRED parameter rather than an optional setter, even though
// it serves exactly one entitlement kind. Forgetting it is not a degraded
// resolver — it is one that reports zero unofficial-WhatsApp numbers for every
// workspace, so nobody can connect one however many they were granted, and
// nothing anywhere would say why. A nil value is still tolerated at runtime (it
// resolves to zero, the safe answer), but the signature makes leaving it out a
// decision rather than an oversight.
func NewEntitlementResolver(
	subscriptions workspace_plan.CurrentSubscriptionReader,
	plans workspace_plan.PlanReader,
	addons workspace_addon.AddonSubscriptionReader,
	configs includedInstanceReader,
) workspace_addon.EntitlementResolver {
	return &entitlementResolver{
		subscriptions: subscriptions,
		plans:         plans,
		addons:        addons,
		configs:       configs,
		now:           utcNow,
	}
}

func (r *entitlementResolver) Resolve(workspaceID string, kind workspace_addon.EntitlementKind) (int, error) {
	base, err := r.planBase(workspaceID, kind)
	if err != nil {
		return 0, err
	}
	subs, err := r.addons.ListActiveByWorkspaceAndKind(workspaceID, kind)
	if err != nil {
		return 0, err
	}
	total := base
	for _, s := range subs {
		total += s.GrantedUnits()
	}
	return total, nil
}

func (r *entitlementResolver) planBase(workspaceID string, kind workspace_addon.EntitlementKind) (int, error) {
	// The one kind whose base is per-workspace configuration rather than the
	// plan, and therefore the one kind that does NOT require an active
	// subscription to have a base. An allowance a platform admin granted
	// explicitly should not evaporate because a plan lapsed — revoking it is an
	// action somebody takes, and the addon top-up below still follows the normal
	// active-subscription rules.
	if kind == workspace_addon.EntitlementUnofficialWhatsAppInstances {
		return readIncludedInstances(r.configs, workspaceID)
	}

	sub, err := r.subscriptions.GetCurrentByWorkspaceID(workspaceID, r.now())
	if err != nil {
		return 0, err
	}
	if sub == nil {
		return 0, workspace_plan.ErrSubscriptionNotCurrent
	}
	if sub.Status != workspace_plan.SubscriptionStatusActive {
		return 0, workspace_plan.ErrSubscriptionNotActive
	}
	plan, err := r.plans.GetByID(sub.PlanDefinitionID)
	if err != nil {
		return 0, err
	}
	if plan == nil {
		return 0, workspace_plan.ErrPlanNotFound
	}
	return planBaseForKind(plan, kind)
}

func planBaseForKind(plan *workspace_plan.PlanDefinition, kind workspace_addon.EntitlementKind) (int, error) {
	switch kind {
	case workspace_addon.EntitlementCallChannels:
		return plan.MaxCallChannels, nil
	case workspace_addon.EntitlementWhatsAppBusinessPhones:
		return plan.IncludedWhatsAppBusinessPhones, nil
	case workspace_addon.EntitlementBranches:
		return plan.MaxBranches, nil
	case workspace_addon.EntitlementUnofficialWhatsAppInstances:
		// Handled before the plan is ever loaded; reaching here means the caller
		// bypassed planBase, and answering with a plan field that does not exist
		// would be worse than saying so.
		return 0, workspace_addon.ErrInvalidEntitlementKind
	default:
		return 0, workspace_addon.ErrInvalidEntitlementKind
	}
}
