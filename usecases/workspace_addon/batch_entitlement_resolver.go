package workspace_addon_usecase

import (
	"time"

	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

// batchSubscriptionReader and batchAddonReader are satisfied implicitly by the
// concrete subscription / addon repositories. Keeping them narrow here avoids
// widening the per-workspace domain reader interfaces (and their fakes).
type batchSubscriptionReader interface {
	GetCurrentByWorkspaceIDs(workspaceIDs []string, at time.Time) (map[string]*workspace_plan.WorkspaceSubscription, error)
}

type batchAddonReader interface {
	SumActiveGrantedUnitsByWorkspaceIDs(workspaceIDs []string, kind workspace_addon.EntitlementKind) (map[string]int, error)
}

type batchEntitlementResolver struct {
	subscriptions batchSubscriptionReader
	plans         workspace_plan.PlanReader
	addons        batchAddonReader
	now           clockFn
}

func NewBatchEntitlementResolver(
	subscriptions batchSubscriptionReader,
	plans workspace_plan.PlanReader,
	addons batchAddonReader,
) workspace_addon.BatchEntitlementResolver {
	return &batchEntitlementResolver{
		subscriptions: subscriptions,
		plans:         plans,
		addons:        addons,
		now:           utcNow,
	}
}

func (r *batchEntitlementResolver) ResolveMany(workspaceIDs []string, kind workspace_addon.EntitlementKind) (map[string]int, error) {
	out := make(map[string]int, len(workspaceIDs))
	if len(workspaceIDs) == 0 {
		return out, nil
	}
	now := r.now()
	subs, err := r.subscriptions.GetCurrentByWorkspaceIDs(workspaceIDs, now)
	if err != nil {
		return nil, err
	}
	addonUnits, err := r.addons.SumActiveGrantedUnitsByWorkspaceIDs(workspaceIDs, kind)
	if err != nil {
		return nil, err
	}
	planCache := map[string]*workspace_plan.PlanDefinition{}
	for _, ws := range workspaceIDs {
		base := 0
		// Plan base counts only while the subscription is active; no plan (or an
		// inactive one) means base 0, mirroring the per-workspace resolver and the
		// GetWorkspaceEntitlements semantics.
		if sub, ok := subs[ws]; ok && sub != nil && sub.Status == workspace_plan.SubscriptionStatusActive {
			plan, cached := planCache[sub.PlanDefinitionID]
			if !cached {
				p, perr := r.plans.GetByID(sub.PlanDefinitionID)
				if perr != nil {
					return nil, perr
				}
				plan = p
				planCache[sub.PlanDefinitionID] = p
			}
			if plan != nil {
				if b, kerr := planBaseForKind(plan, kind); kerr == nil {
					base = b
				}
			}
		}
		out[ws] = base + addonUnits[ws]
	}
	return out, nil
}
