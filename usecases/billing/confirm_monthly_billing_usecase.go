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

// NewConfirmMonthlyBillingUseCase advances the plan and active addon subscriptions to the next
// anchor on confirmed payment of a unified monthly invoice.
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

// WithReactivation wires the entitlement-change handler so a payment that lands after the cancel sweep
// already suspended the workspace's channels reactivates them at the vendor (the lost-cancellation
// inverse). Without it, the confirm still extends the subscriptions; only the channel reactivation is
// skipped. Returns the use case for chaining at wiring.
func (uc *confirmMonthlyBillingUseCase) WithReactivation(handler workspace_addon.EntitlementChangeHandler) *confirmMonthlyBillingUseCase {
	uc.onChange = handler
	return uc
}

// Execute extends the workspace's plan and the addons this invoice covers to the next anchor. It is
// invoked once per confirmed monthly invoice (the webhook's MarkPaid transition and durable webhook
// dedup guard against a redelivery extending a second time). A missing plan subscription is logged and
// skipped rather than failing, so addon extension still proceeds.
//
// Both `Extend` calls flip a subscription back to active, so a payment that arrives after the cancel
// sweep already expired the plan and addons revives them. When that happens (a previously expired
// subscription is brought back), the channels the sweep suspended are reactivated at 360dialog via the
// entitlement handler, which now sees the restored entitlement. That step is best-effort and never
// fails the confirmed payment.
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

	// Restore from the prior anchor: active addons (the on-time path) plus addons this cycle's sweep
	// expired (the late-payment path). A long-lapsed addon from an earlier cycle is excluded.
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
