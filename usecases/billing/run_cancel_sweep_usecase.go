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
	notices   BillingNotices
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
	notices BillingNotices,
) *cancelSweepUseCase {
	return &cancelSweepUseCase{
		invoices:  invoices,
		subs:      subs,
		addons:    addons,
		onReduced: onReduced,
		alerter:   alerter,
		notices:   notices,
		cutoffDay: billing.DefaultCancelDeadlineDay,
		batchSize: defaultEmitBatchSize,
		now:       brtNow,
	}
}

func (uc *cancelSweepUseCase) Execute() (int, error) {
	now := uc.now().In(billing.LocationBRT())

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
			if !uc.pastCutoff(inv, now) {
				uc.warnIfNear(inv, now)
				continue
			}
			cut, err := uc.sweepWorkspace(inv)
			if err != nil {
				log.Printf("[billing-sweep] workspace %s cancellation FAILED for invoice %s: %v", inv.WorkspaceID, inv.ID, err)
				_ = uc.alerter.Alert(context.Background(),
					"billing: channel cancellation failed",
					fmt.Sprintf("workspace %s, invoice %s: %v", inv.WorkspaceID, inv.ID, err))
				continue
			}
			if cut {
				uc.announceCut(inv)
			}
			swept++
		}
		if len(batch) < uc.batchSize {
			break
		}
	}
	return swept, nil
}

func (uc *cancelSweepUseCase) pastCutoff(inv *invoice.Invoice, now time.Time) bool {
	return now.After(uc.cutoffOf(inv, now))
}

func (uc *cancelSweepUseCase) cutoffOf(inv *invoice.Invoice, now time.Time) time.Time {
	month := now
	if inv.DueDate != nil {
		month = inv.DueDate.In(billing.LocationBRT())
	}
	return billing.CancelCutoff(month, billing.LocationBRT(), uc.cutoffDay)
}

func (uc *cancelSweepUseCase) warnIfNear(inv *invoice.Invoice, now time.Time) {
	if inv.DueDate == nil {
		return
	}
	cutoff := uc.cutoffOf(inv, now)
	if !billing.InCutoffWarning(now, *inv.DueDate, cutoff) || uc.replaced(inv) {
		return
	}
	if err := uc.notices.CutoffNear(inv, cutoff); err != nil {
		log.Printf("[billing-sweep] cutoff warning for invoice %s failed, the next run retries it: %v", inv.ID, err)
	}
}

func (uc *cancelSweepUseCase) announceCut(inv *invoice.Invoice) {
	if err := uc.notices.AccessCut(inv); err != nil {
		log.Printf("[billing-sweep] access cut notice for invoice %s failed: %v", inv.ID, err)
	}
}

func (uc *cancelSweepUseCase) replaced(inv *invoice.Invoice) bool {
	sub, err := uc.subs.GetLatestByWorkspaceID(inv.WorkspaceID)
	return err == nil && supersedes(sub, inv)
}

func supersedes(sub *workspace_plan.WorkspaceSubscription, inv *invoice.Invoice) bool {
	if sub == nil || inv == nil || inv.DueDate == nil {
		return false
	}
	return !sub.CurrentPeriodStart.Before(*inv.DueDate)
}

func (uc *cancelSweepUseCase) sweepWorkspace(inv *invoice.Invoice) (bool, error) {
	ws := inv.WorkspaceID

	if sub, err := uc.subs.GetLatestByWorkspaceID(ws); err == nil && supersedes(sub, inv) {
		log.Printf("[billing-sweep] workspace %s keeps subscription %s (period starts %s, invoice %s was due %s); marking only the invoice",
			ws, sub.ID, sub.CurrentPeriodStart.Format(time.DateOnly), inv.ID, inv.DueDate.Format(time.DateOnly))
		return false, uc.invoices.UpdateStatus(inv.ID, invoice.StatusExpired)
	}

	addons, err := uc.addons.ListActiveByWorkspace(ws)
	if err != nil {
		return false, fmt.Errorf("list addons: %w", err)
	}
	for _, a := range addons {
		a.Status = workspace_plan.SubscriptionStatusExpired
		if err := uc.addons.Update(a); err != nil {
			return false, fmt.Errorf("lapse addon %s: %w", a.ID, err)
		}
	}

	if sub, err := uc.subs.GetLatestByWorkspaceID(ws); err == nil && sub != nil {
		if sub.Status != workspace_plan.SubscriptionStatusExpired {
			sub.Status = workspace_plan.SubscriptionStatusExpired
			if err := uc.subs.Update(sub); err != nil {
				return false, fmt.Errorf("expire plan subscription: %w", err)
			}
		}
	}

	if err := uc.onReduced.OnEntitlementReduced(ws, workspace_addon.EntitlementWhatsAppBusinessPhones); err != nil {
		return false, fmt.Errorf("cancel channels at vendor: %w", err)
	}

	if err := uc.invoices.UpdateStatus(inv.ID, invoice.StatusExpired); err != nil {
		return false, fmt.Errorf("mark invoice expired: %w", err)
	}
	return true, nil
}
