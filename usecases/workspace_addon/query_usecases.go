package workspace_addon_usecase

import (
	"errors"

	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type listWorkspaceAddonsUseCase struct {
	subs workspace_addon.AddonSubscriptionRepository
}

func NewListWorkspaceAddonsUseCase(subs workspace_addon.AddonSubscriptionRepository) workspace_addon.ListWorkspaceAddonsUseCase {
	return &listWorkspaceAddonsUseCase{subs: subs}
}

func (uc *listWorkspaceAddonsUseCase) Execute(workspaceID string) ([]*workspace_addon.AddonSubscription, error) {
	return uc.subs.ListActiveByWorkspace(workspaceID)
}

type listAvailableAddonsUseCase struct {
	defs workspace_addon.AddonDefinitionRepository
}

func NewListAvailableAddonsUseCase(defs workspace_addon.AddonDefinitionRepository) workspace_addon.ListAvailableAddonsUseCase {
	return &listAvailableAddonsUseCase{defs: defs}
}

func (uc *listAvailableAddonsUseCase) Execute(workspaceID string) ([]*workspace_addon.AddonDefinition, error) {
	return uc.defs.ListActiveVisible(workspaceID)
}

type getWorkspaceEntitlementsUseCase struct {
	subscriptions workspace_plan.CurrentSubscriptionReader
	plans         workspace_plan.PlanReader
	addons        workspace_addon.AddonSubscriptionReader
	configs       includedInstanceReader
	now           clockFn
}

// NewGetWorkspaceEntitlementsUseCase builds the entitlements read.
//
// This is what the provisioning gates consult, so `configs` being absent means
// the unofficial-WhatsApp entitlement reports zero and nobody can connect a
// number. Required for that reason.
func NewGetWorkspaceEntitlementsUseCase(
	subscriptions workspace_plan.CurrentSubscriptionReader,
	plans workspace_plan.PlanReader,
	addons workspace_addon.AddonSubscriptionReader,
	configs includedInstanceReader,
) workspace_addon.GetWorkspaceEntitlementsUseCase {
	return &getWorkspaceEntitlementsUseCase{
		subscriptions: subscriptions,
		plans:         plans,
		addons:        addons,
		configs:       configs,
		now:           utcNow,
	}
}

func (uc *getWorkspaceEntitlementsUseCase) Execute(workspaceID string) ([]workspace_addon.WorkspaceEntitlement, error) {
	var plan *workspace_plan.PlanDefinition
	sub, err := uc.subscriptions.GetCurrentByWorkspaceID(workspaceID, uc.now())
	if err == nil && sub != nil && sub.Status == workspace_plan.SubscriptionStatusActive {
		p, perr := uc.plans.GetByID(sub.PlanDefinitionID)
		if perr != nil {
			return nil, perr
		}
		plan = p
	} else if err != nil && !errors.Is(err, workspace_plan.ErrSubscriptionNotCurrent) && !errors.Is(err, workspace_plan.ErrSubscriptionNotFound) {
		return nil, err
	}

	kinds := workspace_addon.AllEntitlementKinds()
	out := make([]workspace_addon.WorkspaceEntitlement, 0, len(kinds))
	for _, kind := range kinds {
		base := 0
		if kind == workspace_addon.EntitlementUnofficialWhatsAppInstances {
			// Per-workspace configuration, not the plan, and readable without an
			// active subscription. See the entitlement kind's own comment.
			b, berr := readIncludedInstances(uc.configs, workspaceID)
			if berr != nil {
				return nil, berr
			}
			base = b
		} else if plan != nil {
			if b, kerr := planBaseForKind(plan, kind); kerr == nil {
				base = b
			}
		}
		subs, serr := uc.addons.ListActiveByWorkspaceAndKind(workspaceID, kind)
		if serr != nil {
			return nil, serr
		}
		addonUnits := 0
		for _, s := range subs {
			addonUnits += s.GrantedUnits()
		}
		out = append(out, workspace_addon.WorkspaceEntitlement{
			Kind:       kind,
			PlanBase:   base,
			AddonUnits: addonUnits,
			Total:      base + addonUnits,
		})
	}
	return out, nil
}
