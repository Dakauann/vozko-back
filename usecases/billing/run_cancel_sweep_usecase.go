package billing_usecase

import (
	"context"
	"fmt"
	"log"

	billing "vozko/domain/billing"
	"vozko/domain/invoice"
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type cancelSweepUseCase struct {
	invoices  invoice.Repository
	subs      workspace_plan.SubscriptionRepository
	addons    workspace_addon.AddonSubscriptionRepository
	onReduced workspace_addon.EntitlementChangeHandler
	alerter   billing.OpsAlerter
	cutoffDay int
	batchSize int
	now       clockFn
}

// NewCancelSweepUseCase builds the day-27 cancel sweep. It registers a 360dialog
// cancellation_request for every channel whose unified monthly invoice went unpaid through dunning,
// during the current month, so the vendor never bills the next month for an uncollected channel.
func NewCancelSweepUseCase(
	invoices invoice.Repository,
	subs workspace_plan.SubscriptionRepository,
	addons workspace_addon.AddonSubscriptionRepository,
	onReduced workspace_addon.EntitlementChangeHandler,
	alerter billing.OpsAlerter,
) *cancelSweepUseCase {
	return &cancelSweepUseCase{
		invoices:  invoices,
		subs:      subs,
		addons:    addons,
		onReduced: onReduced,
		alerter:   alerter,
		cutoffDay: billing.DefaultCancelDeadlineDay,
		batchSize: defaultEmitBatchSize,
		now:       brtNow,
	}
}

// Execute cancels the channels of every workspace whose MONTHLY_BILLING invoice is still unpaid,
// streaming through all of them with keyset pagination. It only runs on or after the cutoff day, so
// it never cancels during the dunning grace. A workspace whose vendor cancellation fails is left with
// its invoice still unpaid (so the next sweep retries it) and raises a high-severity ops alert (an
// unconfirmed cancellation is the worst financial case). Returns the number of workspaces swept.
func (uc *cancelSweepUseCase) Execute() (int, error) {
	now := uc.now().In(billing.LocationBRT())
	if now.Day() < uc.cutoffDay {
		return 0, nil // before the cutoff, the dunning grace is still open; do not cancel
	}

	swept := 0
	afterID := ""
	for {
		batch, err := uc.invoices.ListUnpaidByPurpose(invoice.PurposeMonthlyBilling, afterID, uc.batchSize)
		if err != nil {
			return swept, err
		}
		if len(batch) == 0 {
			break
		}
		for i := range batch {
			inv := &batch[i]
			afterID = inv.ID // advance past every row, including failed ones
			if err := uc.sweepWorkspace(inv); err != nil {
				// Leave the invoice unpaid so the next sweep retries; alert loudly because an
				// unconfirmed cancellation keeps the platform paying the vendor for an uncollected channel.
				log.Printf("[billing-sweep] workspace %s cancellation FAILED for invoice %s: %v", inv.WorkspaceID, inv.ID, err)
				_ = uc.alerter.Alert(context.Background(),
					"billing: channel cancellation failed",
					fmt.Sprintf("workspace %s, invoice %s: %v", inv.WorkspaceID, inv.ID, err))
				continue
			}
			swept++
		}
		if len(batch) < uc.batchSize {
			break
		}
	}
	return swept, nil
}

func (uc *cancelSweepUseCase) sweepWorkspace(inv *invoice.Invoice) error {
	ws := inv.WorkspaceID

	// 1. Lapse the workspace's active addons (the whole invoice is unpaid, so nothing is funded).
	addons, err := uc.addons.ListActiveByWorkspace(ws)
	if err != nil {
		return fmt.Errorf("list addons: %w", err)
	}
	for _, a := range addons {
		a.Status = workspace_plan.SubscriptionStatusExpired
		if err := uc.addons.Update(a); err != nil {
			return fmt.Errorf("lapse addon %s: %w", a.ID, err)
		}
	}

	// 2. Expire the plan subscription so plan-included channels are uncovered too (entitlement -> 0).
	if sub, err := uc.subs.GetLatestByWorkspaceID(ws); err == nil && sub != nil {
		if sub.Status != workspace_plan.SubscriptionStatusExpired {
			sub.Status = workspace_plan.SubscriptionStatusExpired
			if err := uc.subs.Update(sub); err != nil {
				return fmt.Errorf("expire plan subscription: %w", err)
			}
		}
	}

	// 3. Cancel the now-uncovered channels at 360dialog (the vendor-cost guard, the point of the sweep).
	if err := uc.onReduced.OnEntitlementReduced(ws, workspace_addon.EntitlementWhatsAppBusinessPhones); err != nil {
		return fmt.Errorf("cancel channels at vendor: %w", err)
	}

	// 4. Only now, after a confirmed cancellation, mark the invoice so it is not re-swept.
	if err := uc.invoices.UpdateStatus(inv.ID, invoice.StatusExpired); err != nil {
		return fmt.Errorf("mark invoice expired: %w", err)
	}
	return nil
}
