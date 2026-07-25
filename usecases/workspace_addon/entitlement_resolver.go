package workspace_addon_usecase

import (
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type entitlementResolver struct {
	subscriptions workspace_plan.CurrentSubscriptionReader
	plans         workspace_plan.PlanReader
	addons        workspace_addon.AddonSubscriptionReader
	now           clockFn
}

func NewEntitlementResolver(
	subscriptions workspace_plan.CurrentSubscriptionReader,
	plans workspace_plan.PlanReader,
	addons workspace_addon.AddonSubscriptionReader,
) workspace_addon.EntitlementResolver {
	return &entitlementResolver{
		subscriptions: subscriptions,
		plans:         plans,
		addons:        addons,
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
	default:
		return 0, workspace_addon.ErrInvalidEntitlementKind
	}
}
