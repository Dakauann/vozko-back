package billing_usecase

import (
	"fmt"
	"log"
	"math"
	"time"

	billing "vozko/domain/billing"
	"vozko/domain/invoice"
	"vozko/domain/workspace"
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

const defaultEmitBatchSize = 500

type emitMonthlyInvoicesUseCase struct {
	subs          workspace_plan.SubscriptionRepository
	plans         workspace_plan.PlanReader
	addons        workspace_addon.AddonSubscriptionRepository
	workspaces    workspace.Repository
	pricing       workspace_pricing.Repository
	createInvoice invoice.CreateInvoiceUseCase
	billingType   string
	dueDay        int
	batchSize     int
	now           clockFn
}

func NewEmitMonthlyInvoicesUseCase(
	subs workspace_plan.SubscriptionRepository,
	plans workspace_plan.PlanReader,
	addons workspace_addon.AddonSubscriptionRepository,
	workspaces workspace.Repository,
	pricing workspace_pricing.Repository,
	createInvoice invoice.CreateInvoiceUseCase,
) *emitMonthlyInvoicesUseCase {
	return &emitMonthlyInvoicesUseCase{
		subs:          subs,
		plans:         plans,
		addons:        addons,
		workspaces:    workspaces,
		pricing:       pricing,
		createInvoice: createInvoice,
		billingType:   "PIX",
		dueDay:        billing.DefaultDueDay,
		batchSize:     defaultEmitBatchSize,
		now:           brtNow,
	}
}

func (uc *emitMonthlyInvoicesUseCase) Execute() (int, error) {
	now := uc.now().In(billing.LocationBRT())
	windowEnd := billing.NextAnchor(now, uc.dueDay)
	rate := uc.exchangeRate()

	emitted := 0
	afterID := ""
	for {
		page, err := uc.subs.ListActiveBillingDue(windowEnd, afterID, uc.batchSize)
		if err != nil {
			return emitted, err
		}
		if len(page) == 0 {
			break
		}
		for _, sub := range page {
			afterID = sub.ID
			created, err := uc.emitForWorkspace(sub, windowEnd, rate)
			if err != nil {
				log.Printf("[billing-emit] workspace %s emit failed: %v", sub.WorkspaceID, err)
				continue
			}
			if created {
				emitted++
			}
		}
		if len(page) < uc.batchSize {
			break
		}
	}
	return emitted, nil
}

func (uc *emitMonthlyInvoicesUseCase) exchangeRate() float64 {
	items, err := uc.pricing.ListDefaultPricingItems()
	if err != nil {
		log.Printf("[billing-emit] WARNING: exchange rate fetch failed, using fallback %.1f: %v", workspace_pricing.DefaultUSDToBRL, err)
		return workspace_pricing.DefaultUSDToBRL
	}
	return workspace_pricing.USDToBRLRate(items)
}

func (uc *emitMonthlyInvoicesUseCase) emitForWorkspace(sub *workspace_plan.WorkspaceSubscription, anchor time.Time, rate float64) (bool, error) {
	plan, err := uc.plans.GetByID(sub.PlanDefinitionID)
	if err != nil {
		return false, fmt.Errorf("get plan: %w", err)
	}

	addons, err := uc.addons.ListActiveByWorkspace(sub.WorkspaceID)
	if err != nil {
		return false, fmt.Errorf("list addons: %w", err)
	}

	addonUSD := make([]int64, 0, len(addons))
	for _, a := range addons {
		addonUSD = append(addonUSD, a.UnitPriceMicros*int64(a.Quantity))
	}

	planBRLCents := sub.BillingCycle.TotalPriceBRLCents(plan.BasePriceBRLCents)
	totalBRL, creditableBRL := billing.MonthlyChargeBRL(planBRLCents, addonUSD, rate)
	if totalBRL <= 0 {
		return false, nil
	}

	lineItems := make([]invoice.InvoiceLineItem, 0, len(addons)+1)
	if creditableBRL > 0 {
		lineItems = append(lineItems, invoice.InvoiceLineItem{
			Kind:       invoice.LineItemPlan,
			Label:      plan.Name,
			AmountBRL:  creditableBRL,
			Creditable: true,
		})
	}
	for _, a := range addons {
		lineBRL := math.Round(float64(a.UnitPriceMicros*int64(a.Quantity))/1_000_000*rate*100) / 100
		lineItems = append(lineItems, invoice.InvoiceLineItem{
			Kind:      invoice.LineItemChannel,
			Label:     a.AddonKey,
			AmountBRL: lineBRL,
			Quantity:  a.Quantity,
		})
	}

	ws, err := uc.workspaces.GetWorkspaceByID(sub.WorkspaceID)
	if err != nil {
		return false, fmt.Errorf("get workspace: %w", err)
	}
	if ws == nil || ws.OwnerID == "" {
		return false, fmt.Errorf("workspace %s has no owner to bill", sub.WorkspaceID)
	}

	key := fmt.Sprintf("monthly:%s:%s", sub.WorkspaceID, anchor.In(billing.LocationBRT()).Format("2006-01-02"))

	_, err = uc.createInvoice.Execute(invoice.CreateInvoiceInput{
		WorkspaceID:      sub.WorkspaceID,
		UserID:           ws.OwnerID,
		Purpose:          invoice.PurposeMonthlyBilling,
		PlanDefinitionID: sub.PlanDefinitionID,
		AmountBRL:        totalBRL,
		CreditableBRL:    creditableBRL,
		BillingType:      uc.billingType,
		BillingCycle:     string(sub.BillingCycle),
		IdempotencyKey:   key,
		DueDate:          &anchor,
		LineItems:        lineItems,
	})
	if err != nil {
		return false, fmt.Errorf("create invoice: %w", err)
	}
	return true, nil
}
