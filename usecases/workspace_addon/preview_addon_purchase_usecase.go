package workspace_addon_usecase

import (
	"errors"

	billing "vozko/domain/billing"
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type previewAddonPurchaseUseCase struct {
	defs workspace_addon.AddonDefinitionRepository
	subs workspace_addon.AddonSubscriptionRepository
	now  clockFn
}

func NewPreviewAddonPurchaseUseCase(
	defs workspace_addon.AddonDefinitionRepository,
	subs workspace_addon.AddonSubscriptionRepository,
) workspace_addon.PreviewAddonPurchaseUseCase {
	return &previewAddonPurchaseUseCase{defs: defs, subs: subs, now: utcNow}
}

// Execute quotes an addon purchase without charging or persisting anything. It resolves the same
// new-vs-top-up state as the purchase and calls the same billing.ActivationPeriod, so the previewed
// ChargeNowMicros is exactly what PurchaseAddonUseCase would charge for the same inputs.
func (uc *previewAddonPurchaseUseCase) Execute(workspaceID string, input workspace_addon.PurchaseAddonInput) (*workspace_addon.AddonPurchasePreview, error) {
	if workspaceID == "" || input.AddonDefinitionID == "" {
		return nil, workspace_addon.ErrInvalidAddonSubscription
	}
	qty := input.Quantity
	if qty <= 0 {
		qty = 1
	}
	cycle := input.BillingCycle
	if !cycle.IsValid() {
		cycle = workspace_plan.BillingCycleMonthly
	}

	def, err := uc.defs.GetByID(input.AddonDefinitionID)
	if err != nil {
		return nil, err
	}
	if def.IsArchived() || !def.IsActive {
		return nil, workspace_addon.ErrAddonInactive
	}

	existing, err := uc.subs.GetActiveByWorkspaceAndDefinition(workspaceID, def.ID)
	if err != nil && !errors.Is(err, workspace_addon.ErrAddonSubscriptionNotFound) {
		return nil, err
	}
	isNew := existing == nil

	now := uc.now()
	full := def.PriceMicros(cycle) * int64(qty)
	charge, periodEnd := billing.ActivationPeriod(now, billing.DefaultEmitDay, billing.DefaultDueDay, cycle.PeriodMonths(), isNew, full)

	prorated := isNew && cycle != workspace_plan.BillingCycleAnnual
	proratedDays := 0
	if prorated {
		proratedDays = billing.ActivationProRataDays(now, billing.DefaultEmitDay, billing.DefaultDueDay)
	}

	return &workspace_addon.AddonPurchasePreview{
		ChargeNowMicros: charge,
		RecurringMicros: full,
		BillingCycle:    cycle,
		Prorated:        prorated,
		ProratedDays:    proratedDays,
		PeriodEnd:       periodEnd,
		// The first unified invoice bills this channel on its first billing anchor, which is exactly the
		// co-term period end. Recurring charges follow on the anchor from there.
		NextInvoiceDate: periodEnd,
	}, nil
}
