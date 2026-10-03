package billing_usecase

import (
	"context"
	"fmt"
	"log"
	"time"

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

func (uc *cancelSweepUseCase) Execute() (int, error) {
	now := uc.now().In(billing.LocationBRT())
	if now.Day() < uc.cutoffDay {
		return 0, nil
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
			afterID = inv.ID
			if err := uc.sweepWorkspace(inv); err != nil {
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

// supersedes reports whether the workspace has already paid for a period that
// starts at or after the unpaid invoice fell due. The invoice belongs to an
// earlier period, so it must not cancel the plan the workspace bought since.
func supersedes(sub *workspace_plan.WorkspaceSubscription, inv *invoice.Invoice) bool {
	if sub == nil || inv == nil || inv.DueDate == nil {
		return false
	}
	return !sub.CurrentPeriodStart.Before(*inv.DueDate)
}

func (uc *cancelSweepUseCase) sweepWorkspace(inv *invoice.Invoice) error {
	ws := inv.WorkspaceID

	if sub, err := uc.subs.GetLatestByWorkspaceID(ws); err == nil && supersedes(sub, inv) {
		log.Printf("[billing-sweep] workspace %s keeps subscription %s (period starts %s, invoice %s was due %s); marking only the invoice",
			ws, sub.ID, sub.CurrentPeriodStart.Format(time.DateOnly), inv.ID, inv.DueDate.Format(time.DateOnly))
		return uc.invoices.UpdateStatus(inv.ID, invoice.StatusExpired)
	}

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

	if sub, err := uc.subs.GetLatestByWorkspaceID(ws); err == nil && sub != nil {
		if sub.Status != workspace_plan.SubscriptionStatusExpired {
			sub.Status = workspace_plan.SubscriptionStatusExpired
			if err := uc.subs.Update(sub); err != nil {
				return fmt.Errorf("expire plan subscription: %w", err)
			}
		}
	}

	if err := uc.onReduced.OnEntitlementReduced(ws, workspace_addon.EntitlementWhatsAppBusinessPhones); err != nil {
		return fmt.Errorf("cancel channels at vendor: %w", err)
	}

	if err := uc.invoices.UpdateStatus(inv.ID, invoice.StatusExpired); err != nil {
		return fmt.Errorf("mark invoice expired: %w", err)
	}
	return nil
}
