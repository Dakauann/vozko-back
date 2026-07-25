package billing_usecase

import (
	"fmt"
	"strings"
	"time"

	billing "vozko/domain/billing"
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

// WorkspaceBillingPreview is one workspace's projected first unified charge, computed read-only.
type WorkspaceBillingPreview struct {
	WorkspaceID      string
	CurrentPeriodEnd time.Time
	BillingAnchor    time.Time // the anchor (due date) the workspace would be billed for
	PlanBRLCents     int64
	AddonCount       int
	TotalBRL         float64
	CreditableBRL    float64
}

// BillingPreviewReport is the dry-run of the next emit cycle: exactly what each active workspace would
// be charged, with no invoices created and no writes. It is the pre-cutover "what will happen" preview.
type BillingPreviewReport struct {
	Rows        []WorkspaceBillingPreview
	SkippedZero int // active subscriptions with nothing billable (free plan, no addons)
	TotalBRL    float64
}

type previewMonthlyBillingUseCase struct {
	subs      workspace_plan.SubscriptionRepository
	plans     workspace_plan.PlanReader
	addons    workspace_addon.AddonSubscriptionRepository
	pricing   workspace_pricing.Repository
	dueDay    int
	batchSize int
	now       clockFn
}

// NewPreviewMonthlyBillingUseCase builds the read-only cutover dry-run. It takes the same reads as the
// emitter minus the invoice writer, so the preview is guaranteed to match what emit will actually do.
func NewPreviewMonthlyBillingUseCase(
	subs workspace_plan.SubscriptionRepository,
	plans workspace_plan.PlanReader,
	addons workspace_addon.AddonSubscriptionRepository,
	pricing workspace_pricing.Repository,
) *previewMonthlyBillingUseCase {
	return &previewMonthlyBillingUseCase{
		subs:      subs,
		plans:     plans,
		addons:    addons,
		pricing:   pricing,
		dueDay:    billing.DefaultDueDay,
		batchSize: defaultEmitBatchSize,
		now:       brtNow,
	}
}

// Execute computes, read-only, the unified charge the next emit cycle would raise for every active
// workspace. It mirrors the emit use case (same due window, same MonthlyChargeBRL), but creates nothing
// and writes nothing, so it is safe to run any time as the pre-cutover preview or an ongoing sanity check.
func (uc *previewMonthlyBillingUseCase) Execute() (BillingPreviewReport, error) {
	now := uc.now().In(billing.LocationBRT())
	windowEnd := billing.NextAnchor(now, uc.dueDay)
	rate := uc.exchangeRate()

	var report BillingPreviewReport
	afterID := ""
	for {
		page, err := uc.subs.ListActiveBillingDue(windowEnd, afterID, uc.batchSize)
		if err != nil {
			return report, err
		}
		if len(page) == 0 {
			break
		}
		for _, sub := range page {
			afterID = sub.ID
			plan, err := uc.plans.GetByID(sub.PlanDefinitionID)
			if err != nil {
				return report, fmt.Errorf("get plan for workspace %s: %w", sub.WorkspaceID, err)
			}
			addons, err := uc.addons.ListActiveByWorkspace(sub.WorkspaceID)
			if err != nil {
				return report, fmt.Errorf("list addons for workspace %s: %w", sub.WorkspaceID, err)
			}
			addonUSD := make([]int64, 0, len(addons))
			for _, a := range addons {
				addonUSD = append(addonUSD, a.UnitPriceMicros*int64(a.Quantity))
			}
			// Cycle-aware, exactly as the emitter charges: annual subscriptions preview the full-year
			// plan price, not a single month, so the dry-run total equals what emit will raise.
			planBRLCents := sub.BillingCycle.TotalPriceBRLCents(plan.BasePriceBRLCents)
			totalBRL, creditableBRL := billing.MonthlyChargeBRL(planBRLCents, addonUSD, rate)
			if totalBRL <= 0 {
				report.SkippedZero++
				continue
			}
			report.Rows = append(report.Rows, WorkspaceBillingPreview{
				WorkspaceID:      sub.WorkspaceID,
				CurrentPeriodEnd: sub.CurrentPeriodEnd,
				BillingAnchor:    windowEnd,
				PlanBRLCents:     planBRLCents,
				AddonCount:       len(addons),
				TotalBRL:         totalBRL,
				CreditableBRL:    creditableBRL,
			})
			report.TotalBRL += totalBRL
		}
		if len(page) < uc.batchSize {
			break
		}
	}
	return report, nil
}

// exchangeRate mirrors the emitter's rate fetch: one read, fall back to the default on error.
func (uc *previewMonthlyBillingUseCase) exchangeRate() float64 {
	items, err := uc.pricing.ListDefaultPricingItems()
	if err != nil {
		return workspace_pricing.DefaultUSDToBRL
	}
	return workspace_pricing.USDToBRLRate(items)
}

// Format renders the report as a human-readable table for the cutover dry-run log.
func (r BillingPreviewReport) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "MONTHLY BILLING DRY-RUN: %d workspace(s) would be billed, %d skipped (nothing billable), total R$ %.2f\n",
		len(r.Rows), r.SkippedZero, r.TotalBRL)
	for _, row := range r.Rows {
		fmt.Fprintf(&b, "  ws=%s anchor=%s total=R$ %.2f (plan R$ %.2f + %d addon(s)) saldo=R$ %.2f\n",
			row.WorkspaceID, row.BillingAnchor.Format("2006-01-02"), row.TotalBRL,
			float64(row.PlanBRLCents)/100.0, row.AddonCount, row.CreditableBRL)
	}
	return b.String()
}
