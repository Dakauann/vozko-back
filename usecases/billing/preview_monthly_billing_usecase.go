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

type WorkspaceBillingPreview struct {
	WorkspaceID      string
	CurrentPeriodEnd time.Time
	BillingAnchor    time.Time
	PlanBRLCents     int64
	AddonCount       int
	TotalBRL         float64
	CreditableBRL    float64
}

type BillingPreviewReport struct {
	Rows        []WorkspaceBillingPreview
	SkippedZero int
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

func (uc *previewMonthlyBillingUseCase) exchangeRate() float64 {
	items, err := uc.pricing.ListDefaultPricingItems()
	if err != nil {
		return workspace_pricing.DefaultUSDToBRL
	}
	return workspace_pricing.USDToBRLRate(items)
}

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
