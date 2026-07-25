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

// defaultEmitBatchSize is the keyset page size. The emitter streams through every due workspace in
// pages of this size, so there is no per-run cap regardless of how many workspaces share the anchor.
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

// NewEmitMonthlyInvoicesUseCase builds the monthly emitter on the real domain repositories. The
// default billing type is PIX.
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

// Execute composes and issues one unified MONTHLY_BILLING invoice per active workspace whose plan
// period ends at the upcoming anchor. It streams through all due workspaces with keyset pagination,
// so it scales to any number of customers. Each invoice carries a deterministic idempotency key
// (workspace + anchor period), so a re-run returns the existing invoices without charging Asaas
// again. Per-workspace failures are logged and skipped so one bad workspace cannot stall the rest.
// Returns the number of invoices emitted (or already present).
func (uc *emitMonthlyInvoicesUseCase) Execute() (int, error) {
	// Evaluate the anchor in the billing timezone regardless of the clock's location, so the anchor
	// date (and the idempotency key derived from it) is stable.
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
			afterID = sub.ID // advance the cursor past every row, including skipped or failed ones
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

// exchangeRate fetches the USD->BRL rate once per run, falling back to the default on a fetch error
// (mirroring create_invoice) so a transient pricing read does not block billing.
func (uc *emitMonthlyInvoicesUseCase) exchangeRate() float64 {
	items, err := uc.pricing.ListDefaultPricingItems()
	if err != nil {
		log.Printf("[billing-emit] WARNING: exchange rate fetch failed, using fallback %.1f: %v", workspace_pricing.DefaultUSDToBRL, err)
		return workspace_pricing.DefaultUSDToBRL
	}
	return workspace_pricing.USDToBRLRate(items)
}

// emitForWorkspace composes and issues the workspace's monthly invoice. It returns created=false
// (with a nil error) when there is nothing billable, so a skip is not counted as an emission.
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

	// Charge the plan on its own billing cycle: a monthly subscription pays one month's base, an
	// annual subscription pays the full year (base * 12, less any annual discount). This is the amount
	// `Extend` grants a period for on payment (monthly -> next anchor, annual -> +12 months), so the
	// money charged and the access granted always agree. Using BasePriceBRLCents directly would charge
	// an annual subscriber a single month while extending them a full year.
	planBRLCents := sub.BillingCycle.TotalPriceBRLCents(plan.BasePriceBRLCents)
	totalBRL, creditableBRL := billing.MonthlyChargeBRL(planBRLCents, addonUSD, rate)
	if totalBRL <= 0 {
		return false, nil // nothing billable (free plan, no addons)
	}

	// Customer-facing breakdown, price only: the plan line (credited to saldo) plus one line per channel
	// addon (a vendor-cost pass-through, not credited). Labels are addon keys the frontend localizes.
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

	// Idempotency key: one invoice per workspace per anchor cycle. The anchor period is the upcoming
	// anchor date in the billing timezone, so re-runs within the same cycle return the same invoice.
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
