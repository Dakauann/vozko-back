package workspace_addon_usecase

import (
	"errors"
	"strconv"

	"github.com/google/uuid"

	"vozko/domain/balance"
	billing "vozko/domain/billing"
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type purchaseAddonUseCase struct {
	defs        workspace_addon.AddonDefinitionRepository
	subs        workspace_addon.AddonSubscriptionRepository
	balanceRepo balance.Repository
	onChange    workspace_addon.EntitlementChangeHandler
	now         clockFn
}

func NewPurchaseAddonUseCase(
	defs workspace_addon.AddonDefinitionRepository,
	subs workspace_addon.AddonSubscriptionRepository,
	balanceRepo balance.Repository,
	onChange workspace_addon.EntitlementChangeHandler,
) workspace_addon.PurchaseAddonUseCase {
	return &purchaseAddonUseCase{defs: defs, subs: subs, balanceRepo: balanceRepo, onChange: onChange, now: utcNow}
}

func (uc *purchaseAddonUseCase) Execute(workspaceID string, input workspace_addon.PurchaseAddonInput) (*workspace_addon.AddonSubscription, error) {
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

	now := uc.now()
	unitPrice := def.PriceMicros(cycle)
	unitCost := def.CostMicros(cycle)

	existing, err := uc.subs.GetActiveByWorkspaceAndDefinition(workspaceID, def.ID)
	if err != nil && !errors.Is(err, workspace_addon.ErrAddonSubscriptionNotFound) {
		return nil, err
	}

	isNew := existing == nil

	// A newly added monthly channel is charged a proration for the partial period from now to its first
	// billing anchor (capped at one month) and co-terms to that anchor; annual addons and quantity
	// top-ups of an existing addon keep the full-period behavior. billing.ActivationPeriod is the same
	// function the purchase preview calls, so the amount shown before buying always equals what is charged.
	amount, periodEnd := billing.ActivationPeriod(now, billing.DefaultEmitDay, billing.DefaultDueDay, cycle.PeriodMonths(), isNew, unitPrice*int64(qty))
	cost, _ := billing.ActivationPeriod(now, billing.DefaultEmitDay, billing.DefaultDueDay, cycle.PeriodMonths(), isNew, unitCost*int64(qty))

	var sub *workspace_addon.AddonSubscription
	if existing != nil {
		sub = existing
		sub.Quantity += qty
		sub.UnitsPerQuantity = def.UnitsPerQuantity
		sub.UnitPriceMicros = unitPrice
		sub.BillingCycle = cycle
		sub.CurrentPeriodStart = now
		sub.CurrentPeriodEnd = periodEnd
		sub.Status = workspace_plan.SubscriptionStatusActive
		sub.CancelledAt = nil
		sub.UpdatedAt = now
	} else {
		sub = &workspace_addon.AddonSubscription{
			ID:                 uuid.NewString(),
			WorkspaceID:        workspaceID,
			AddonDefinitionID:  def.ID,
			AddonKey:           def.Key,
			EntitlementKind:    def.EntitlementKind,
			Quantity:           qty,
			UnitsPerQuantity:   def.UnitsPerQuantity,
			BillingCycle:       cycle,
			Status:             workspace_plan.SubscriptionStatusActive,
			UnitPriceMicros:    unitPrice,
			CurrentPeriodStart: now,
			CurrentPeriodEnd:   periodEnd,
		}
	}
	if err := sub.Validate(); err != nil {
		return nil, err
	}

	refID := "addon_purchase:" + sub.ID + ":" + strconv.FormatInt(now.Unix(), 10)
	didDebit, err := uc.debit(workspaceID, refID, "Addon: "+def.Name, amount, cost)
	if err != nil {
		return nil, err
	}

	var persistErr error
	if isNew {
		persistErr = uc.subs.Create(sub)
	} else {
		persistErr = uc.subs.Update(sub)
	}
	if persistErr != nil {
		if didDebit {
			uc.refund(workspaceID, refID, "Estorno addon: "+def.Name, amount, cost)
		}
		return nil, persistErr
	}

	if uc.onChange != nil {
		// Best-effort: a successful purchase that raised the entitlement may
		// reactivate numbers suspended by a prior lapse. Never fail the purchase.
		_ = uc.onChange.OnEntitlementIncreased(workspaceID, def.EntitlementKind)
	}
	return sub, nil
}

func (uc *purchaseAddonUseCase) debit(workspaceID, refID, description string, amount, cost int64) (bool, error) {
	if amount <= 0 {
		return false, nil
	}
	exists, err := uc.balanceRepo.ExistsTransactionByReferenceID(refID)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	if _, err := uc.balanceRepo.EnsureBalanceExists(workspaceID, balanceCurrencyUSD); err != nil {
		return false, err
	}
	if _, err := uc.balanceRepo.DebitBalance(balance.DebitBalanceInput{
		WorkspaceID:  workspaceID,
		Amount:       amount,
		ServiceType:  balance.ServiceAddon,
		ReferenceID:  &refID,
		Description:  description,
		CostMicros:   cost,
		ProfitMicros: amount - cost,
	}); err != nil {
		return false, err
	}
	return true, nil
}

func (uc *purchaseAddonUseCase) refund(workspaceID, refID, description string, amount, cost int64) {
	if amount <= 0 {
		return
	}
	refundRef := "refund:" + refID
	_, _ = uc.balanceRepo.CreditBalance(balance.CreditBalanceInput{
		WorkspaceID:  workspaceID,
		Amount:       amount,
		ServiceType:  balance.ServiceAddon,
		ReferenceID:  &refundRef,
		Description:  description,
		CostMicros:   cost,
		ProfitMicros: -(amount - cost),
		IsRefund:     true,
	})
}
