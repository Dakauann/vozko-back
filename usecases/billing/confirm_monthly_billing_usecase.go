package billing_usecase

import (
	"fmt"
	"log"

	billing "vozko/domain/billing"
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type confirmMonthlyBillingUseCase struct {
	subs     workspace_plan.SubscriptionRepository
	addons   workspace_addon.AddonSubscriptionRepository
	onChange workspace_addon.EntitlementChangeHandler
	dueDay   int
	now      clockFn
}

var _ billing.ConfirmMonthlyBillingUseCase = (*confirmMonthlyBillingUseCase)(nil)

func NewConfirmMonthlyBillingUseCase(
	subs workspace_plan.SubscriptionRepository,
	addons workspace_addon.AddonSubscriptionRepository,
) *confirmMonthlyBillingUseCase {
	return &confirmMonthlyBillingUseCase{
		subs:   subs,
		addons: addons,
		dueDay: billing.DefaultDueDay,
		now:    brtNow,
	}
}

func (uc *confirmMonthlyBillingUseCase) WithReactivation(handler workspace_addon.EntitlementChangeHandler) *confirmMonthlyBillingUseCase {
	uc.onChange = handler
	return uc
}

func (uc *confirmMonthlyBillingUseCase) Execute(workspaceID string) error {
	now := uc.now()
	revived := false

	if sub, err := uc.subs.GetLatestByWorkspaceID(workspaceID); err != nil {
		log.Printf("[billing-confirm] workspace %s: get plan subscription failed: %v", workspaceID, err)
	} else if sub != nil {
		if sub.Status == workspace_plan.SubscriptionStatusExpired {
			revived = true
		}
		sub.Extend(now, uc.dueDay)
		if err := uc.subs.Update(sub); err != nil {
			return fmt.Errorf("update plan subscription: %w", err)
		}
	}

	addons, err := uc.addons.ListReactivatableByWorkspace(workspaceID, now.AddDate(0, -1, 0))
	if err != nil {
		return fmt.Errorf("list reactivatable addons: %w", err)
	}
	for _, a := range addons {
		if a.Status == workspace_plan.SubscriptionStatusExpired {
			revived = true
		}
		a.Extend(now, uc.dueDay)
		if err := uc.addons.Update(a); err != nil {
			return fmt.Errorf("update addon %s: %w", a.ID, err)
		}
	}

	if revived && uc.onChange != nil {
		if err := uc.onChange.OnEntitlementIncreased(workspaceID, workspace_addon.EntitlementWhatsAppBusinessPhones); err != nil {
			log.Printf("[billing-confirm] workspace %s: channel reactivation after late payment failed: %v", workspaceID, err)
		}
	}
	return nil
}
