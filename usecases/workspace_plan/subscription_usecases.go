package workspace_plan_usecase

import (
	"errors"
	"log"
	"time"

	"github.com/google/uuid"

	billing "vozko/domain/billing"
	"vozko/domain/invoice"
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type createSubscriptionInvoiceUseCase struct {
	plans         workspace_plan.PlanRepository
	subscriptions workspace_plan.SubscriptionRepository
	referrals     workspace_plan.WorkspaceReferralReader
	createInvoice invoice.CreateInvoiceUseCase
	now           clockFn
}

type subscribeWorkspaceUseCase struct {
	plans         workspace_plan.PlanRepository
	subscriptions workspace_plan.SubscriptionRepository
	now           clockFn
}

type renewWorkspaceSubscriptionUseCase struct {
	plans         workspace_plan.PlanRepository
	subscriptions workspace_plan.SubscriptionRepository
	now           clockFn
}

type cancelWorkspaceSubscriptionUseCase struct {
	plans         workspace_plan.PlanRepository
	subscriptions workspace_plan.SubscriptionRepository
	now           clockFn
}

type getWorkspaceSubscriptionUseCase struct {
	plans         workspace_plan.PlanRepository
	subscriptions workspace_plan.SubscriptionRepository
	now           clockFn
}

type ensureCurrentWorkspaceSubscriptionUseCase struct {
	subscriptions workspace_plan.SubscriptionRepository
	now           clockFn
}

type ensureActiveWorkspaceSubscriptionUseCase struct {
	current workspace_plan.EnsureCurrentWorkspaceSubscriptionUseCase
}

func NewCreateSubscriptionInvoiceUseCase(
	plans workspace_plan.PlanRepository,
	subscriptions workspace_plan.SubscriptionRepository,
	createInvoice invoice.CreateInvoiceUseCase,
	referrals workspace_plan.WorkspaceReferralReader,
) workspace_plan.CreateSubscriptionInvoiceUseCase {
	return &createSubscriptionInvoiceUseCase{
		plans:         plans,
		subscriptions: subscriptions,
		referrals:     referrals,
		createInvoice: createInvoice,
		now:           utcNow,
	}
}

func NewSubscribeWorkspaceUseCase(plans workspace_plan.PlanRepository, subscriptions workspace_plan.SubscriptionRepository) workspace_plan.SubscribeWorkspaceUseCase {
	return &subscribeWorkspaceUseCase{plans: plans, subscriptions: subscriptions, now: utcNow}
}

func NewRenewWorkspaceSubscriptionUseCase(plans workspace_plan.PlanRepository, subscriptions workspace_plan.SubscriptionRepository) workspace_plan.RenewWorkspaceSubscriptionUseCase {
	return &renewWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subscriptions, now: utcNow}
}

func NewCancelWorkspaceSubscriptionUseCase(plans workspace_plan.PlanRepository, subscriptions workspace_plan.SubscriptionRepository) workspace_plan.CancelWorkspaceSubscriptionUseCase {
	return &cancelWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subscriptions, now: utcNow}
}

func NewGetWorkspaceSubscriptionUseCase(plans workspace_plan.PlanRepository, subscriptions workspace_plan.SubscriptionRepository) workspace_plan.GetWorkspaceSubscriptionUseCase {
	return &getWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subscriptions, now: utcNow}
}

func NewEnsureCurrentWorkspaceSubscriptionUseCase(subscriptions workspace_plan.SubscriptionRepository) workspace_plan.EnsureCurrentWorkspaceSubscriptionUseCase {
	return &ensureCurrentWorkspaceSubscriptionUseCase{subscriptions: subscriptions, now: utcNow}
}

func NewEnsureActiveWorkspaceSubscriptionUseCase(current workspace_plan.EnsureCurrentWorkspaceSubscriptionUseCase) workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase {
	return &ensureActiveWorkspaceSubscriptionUseCase{current: current}
}

func (uc *ensureActiveWorkspaceSubscriptionUseCase) Execute(workspaceID string) (*workspace_plan.WorkspaceSubscription, error) {
	sub, err := uc.current.Execute(workspaceID)
	if err != nil {
		return nil, err
	}
	if sub.Status != workspace_plan.SubscriptionStatusActive {
		return nil, workspace_plan.ErrSubscriptionNotActive
	}
	return sub, nil
}

func (uc *createSubscriptionInvoiceUseCase) Execute(workspaceID, userID string, input workspace_plan.CreateSubscriptionInvoiceInput) (*invoice.CreateInvoiceOutput, error) {
	now := uc.now()
	if workspaceID == "" || userID == "" || input.PlanID == "" {
		return nil, workspace_plan.ErrInvalidSubscription
	}
	var activeSub *workspace_plan.WorkspaceSubscription
	if current, err := uc.subscriptions.GetCurrentByWorkspaceID(workspaceID, now); err == nil && current != nil && current.Status == workspace_plan.SubscriptionStatusActive {
		activeSub = current
	} else if err != nil && !errors.Is(err, workspace_plan.ErrSubscriptionNotCurrent) {
		return nil, err
	}
	if latest, err := uc.subscriptions.GetLatestByWorkspaceID(workspaceID); err == nil && latest != nil && latest.ExpireIfNeeded(now) {
		if updateErr := uc.subscriptions.Update(latest); updateErr != nil {
			return nil, updateErr
		}
	} else if err != nil && !errors.Is(err, workspace_plan.ErrSubscriptionNotFound) {
		return nil, err
	}

	plan, err := uc.plans.GetByID(input.PlanID)
	if err != nil {
		return nil, err
	}
	if plan.IsArchived() {
		return nil, workspace_plan.ErrPlanArchived
	}

	if activeSub != nil {
		if activeSub.PlanDefinitionID == plan.ID {
			return nil, workspace_plan.ErrSubscriptionAlreadyExists
		}
		currentPlan, cpErr := uc.plans.GetByID(activeSub.PlanDefinitionID)
		if cpErr != nil {
			return nil, cpErr
		}
		if plan.BasePriceBRLCents <= currentPlan.BasePriceBRLCents {
			return nil, workspace_plan.ErrDowngradeNotAllowed
		}
	}

	if !plan.IsGloballyVisible {
		ids, idsErr := uc.plans.GetAllowedWorkspaceIDs(plan.ID)
		if idsErr != nil {
			return nil, idsErr
		}
		plan.AllowedWorkspaceIDs = ids
		if plan.IsExclusive() {

			if uc.referrals == nil {
				return nil, workspace_plan.ErrPlanNotVisible
			}
			affiliateID, refErr := uc.referrals.GetAffiliateIDByWorkspaceID(workspaceID)
			if refErr != nil {
				return nil, refErr
			}
			if !plan.IsVisibleToAffiliate(affiliateID) {
				return nil, workspace_plan.ErrPlanNotVisible
			}
		} else if !plan.IsVisibleTo(workspaceID) {
			return nil, workspace_plan.ErrPlanNotVisible
		}
	}
	if plan.BasePriceBRLCents <= 0 {
		return nil, invoice.ErrInvalidAmount
	}

	cycle := workspace_plan.BillingCycleMonthly
	if input.BillingCycle != "" {
		cycle = workspace_plan.BillingCycle(input.BillingCycle)
		if !cycle.IsValid() {
			return nil, workspace_plan.ErrInvalidSubscription
		}
	}

	invoiceAmount := float64(cycle.TotalPriceBRLCents(plan.BasePriceBRLCents)) / 100.0

	description := input.Description
	if description == "" {
		description = "Assinatura do plano " + plan.Name
	}

	return uc.createInvoice.Execute(invoice.CreateInvoiceInput{
		WorkspaceID:      workspaceID,
		UserID:           userID,
		Purpose:          invoice.PurposeSubscription,
		PlanDefinitionID: plan.ID,
		AmountBRL:        invoiceAmount,
		BillingType:      input.BillingType,
		Description:      description,
		BillingCycle:     string(cycle),
		ReferralCode:     input.ReferralCode,
	})
}

func (uc *subscribeWorkspaceUseCase) Execute(workspaceID, planID string, billingCycle workspace_plan.BillingCycle) (*workspace_plan.WorkspaceSubscriptionDetails, error) {
	now := uc.now()
	if workspaceID == "" || planID == "" {
		return nil, workspace_plan.ErrInvalidSubscription
	}
	if current, err := uc.subscriptions.GetCurrentByWorkspaceID(workspaceID, now); err == nil && current != nil {
		if current.Status == workspace_plan.SubscriptionStatusActive {

			currentPlan, cpErr := uc.plans.GetByID(current.PlanDefinitionID)
			newPlan, npErr := uc.plans.GetByID(planID)
			if cpErr != nil || npErr != nil {

				if cpErr == nil && npErr != nil {
					return nil, npErr
				}
				if cpErr != nil {
					return nil, cpErr
				}
			}
			if newPlan.BasePriceBRLCents <= currentPlan.BasePriceBRLCents {
				return nil, workspace_plan.ErrSubscriptionAlreadyExists
			}
			current.Status = workspace_plan.SubscriptionStatusExpired
			current.UpdatedAt = now
			if updateErr := uc.subscriptions.Update(current); updateErr != nil {
				return nil, updateErr
			}
		}

		if current.Status == workspace_plan.SubscriptionStatusCancelled {
			current.Status = workspace_plan.SubscriptionStatusExpired
			current.UpdatedAt = now
			if updateErr := uc.subscriptions.Update(current); updateErr != nil {
				return nil, updateErr
			}
		}
	} else if err != nil && !errors.Is(err, workspace_plan.ErrSubscriptionNotCurrent) {
		return nil, err
	}
	if latest, err := uc.subscriptions.GetLatestByWorkspaceID(workspaceID); err == nil && latest != nil && latest.ExpireIfNeeded(now) {
		if updateErr := uc.subscriptions.Update(latest); updateErr != nil {
			return nil, updateErr
		}
	} else if err != nil && !errors.Is(err, workspace_plan.ErrSubscriptionNotFound) {
		return nil, err
	}

	plan, err := uc.plans.GetByID(planID)
	if err != nil {
		return nil, err
	}
	if plan.IsArchived() {
		return nil, workspace_plan.ErrPlanArchived
	}

	if !billingCycle.IsValid() {
		billingCycle = workspace_plan.BillingCycleMonthly
	}

	subscription := &workspace_plan.WorkspaceSubscription{
		ID:                 uuid.NewString(),
		WorkspaceID:        workspaceID,
		PlanDefinitionID:   plan.ID,
		PlanName:           plan.Name,
		MaxCallChannels:    plan.MaxCallChannels,
		BillingCycle:       billingCycle,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   firstPeriodEnd(now, billingCycle),
	}
	if err := subscription.Validate(); err != nil {
		return nil, err
	}
	if err := uc.subscriptions.Create(subscription); err != nil {
		return nil, err
	}
	return &workspace_plan.WorkspaceSubscriptionDetails{Subscription: subscription, Plan: plan}, nil
}

// firstPeriodEnd returns the end of a new subscription's first billing period. Monthly plans
// co-term to the global billing anchor (BILLING_DUE_DAY) computed in São Paulo time via
// PlanFirstAnchor (which honors the first-cycle floor), so plan and channel charges land on one
// unified monthly date. Annual plans keep a 12-month period; the monthly anchor model does not
// apply to them.
func firstPeriodEnd(now time.Time, cycle workspace_plan.BillingCycle) time.Time {
	if cycle == workspace_plan.BillingCycleAnnual {
		return now.AddDate(0, 12, 0)
	}
	return billing.PlanFirstAnchor(now.In(billing.LocationBRT()), billing.DefaultDueDay, billing.DefaultPlanFirstAnchorFloorDays)
}

// nextPeriodEnd rolls an existing subscription to its next billing period end. Monthly subscriptions
// advance to the next global anchor strictly after `from` (no first-cycle floor, since this is a
// renewal, not a signup), keeping them co-termed to the unified billing day. Annual subscriptions
// advance 12 months.
func nextPeriodEnd(from time.Time, cycle workspace_plan.BillingCycle) time.Time {
	if cycle == workspace_plan.BillingCycleAnnual {
		return from.AddDate(0, 12, 0)
	}
	return billing.NextAnchor(from.In(billing.LocationBRT()).AddDate(0, 0, 1), billing.DefaultDueDay)
}

func (uc *renewWorkspaceSubscriptionUseCase) Execute(workspaceID string) (*workspace_plan.WorkspaceSubscriptionDetails, error) {
	now := uc.now()
	subscription, err := uc.subscriptions.GetLatestByWorkspaceID(workspaceID)
	if err != nil {
		return nil, err
	}
	if subscription.IsCurrent(now) {
		return nil, workspace_plan.ErrSubscriptionAlreadyExists
	}
	subscription.Status = workspace_plan.SubscriptionStatusActive
	subscription.CancelledAt = nil
	subscription.CurrentPeriodStart = now
	subscription.CurrentPeriodEnd = nextPeriodEnd(now, subscription.BillingCycle)
	subscription.UpdatedAt = now
	if err := subscription.Validate(); err != nil {
		return nil, err
	}
	if err := uc.subscriptions.Update(subscription); err != nil {
		return nil, err
	}
	plan, err := uc.plans.GetByID(subscription.PlanDefinitionID)
	if err != nil {
		return nil, err
	}

	if plan.Name != subscription.PlanName || plan.MaxCallChannels != subscription.MaxCallChannels {
		subscription.PlanName = plan.Name
		subscription.MaxCallChannels = plan.MaxCallChannels
		_ = uc.subscriptions.Update(subscription)
	}
	return &workspace_plan.WorkspaceSubscriptionDetails{Subscription: subscription, Plan: plan}, nil
}

func (uc *cancelWorkspaceSubscriptionUseCase) Execute(workspaceID string) (*workspace_plan.WorkspaceSubscriptionDetails, error) {
	now := uc.now()
	subscription, err := uc.subscriptions.GetCurrentByWorkspaceID(workspaceID, now)
	if err != nil {
		return nil, err
	}
	if subscription.Status == workspace_plan.SubscriptionStatusCancelled {
		return nil, workspace_plan.ErrSubscriptionCancelled
	}
	subscription.Status = workspace_plan.SubscriptionStatusCancelled
	subscription.CancelledAt = &now
	subscription.UpdatedAt = now
	if err := uc.subscriptions.Update(subscription); err != nil {
		return nil, err
	}
	plan, err := uc.plans.GetByID(subscription.PlanDefinitionID)
	if err != nil {
		return nil, err
	}
	return &workspace_plan.WorkspaceSubscriptionDetails{Subscription: subscription, Plan: plan}, nil
}

func (uc *getWorkspaceSubscriptionUseCase) Execute(workspaceID string) (*workspace_plan.WorkspaceSubscriptionDetails, error) {
	now := uc.now()
	subscription, err := uc.subscriptions.GetLatestByWorkspaceID(workspaceID)
	if err != nil {
		return nil, err
	}
	if subscription.ExpireIfNeeded(now) {
		if err := uc.subscriptions.Update(subscription); err != nil {
			return nil, err
		}
	}
	plan, err := uc.plans.GetByID(subscription.PlanDefinitionID)
	if err != nil {
		return nil, err
	}
	return &workspace_plan.WorkspaceSubscriptionDetails{Subscription: subscription, Plan: plan}, nil
}

func (uc *ensureCurrentWorkspaceSubscriptionUseCase) Execute(workspaceID string) (*workspace_plan.WorkspaceSubscription, error) {
	now := uc.now()
	subscription, err := uc.subscriptions.GetCurrentByWorkspaceID(workspaceID, now)
	if err == nil {
		return subscription, nil
	}
	if !errors.Is(err, workspace_plan.ErrSubscriptionNotCurrent) {
		return nil, err
	}
	latest, latestErr := uc.subscriptions.GetLatestByWorkspaceID(workspaceID)
	if latestErr != nil {
		if errors.Is(latestErr, workspace_plan.ErrSubscriptionNotFound) {
			return nil, workspace_plan.ErrSubscriptionNotCurrent
		}
		return nil, latestErr
	}
	if latest.ExpireIfNeeded(now) {
		if updateErr := uc.subscriptions.Update(latest); updateErr != nil {
			return nil, updateErr
		}
	}
	return nil, workspace_plan.ErrSubscriptionNotCurrent
}

const defaultExpireBatchSize = 500

// expireDunningGraceDays keeps this job from expiring (and cancelling the channels of) a subscription
// during the unified-billing dunning window. A monthly subscription's period ends on the anchor/due
// day; if unpaid it must stay serving through dunning until the day-27 cancel sweep owns the
// cancellation. Deriving the grace from the knobs (cutoff - due, plus a day of slack) means this job
// only force-expires a subscription that is overdue BEYOND dunning, deferring to the sweep in between.
const expireDunningGraceDays = billing.DefaultCancelDeadlineDay - billing.DefaultDueDay + 1

type expireSubscriptionsUseCase struct {
	subscriptions workspace_plan.SubscriptionRepository
	onReduced     workspace_addon.EntitlementChangeHandler
	batchSize     int
	graceDays     int
	now           clockFn
}

func NewExpireSubscriptionsUseCase(subscriptions workspace_plan.SubscriptionRepository, onReduced workspace_addon.EntitlementChangeHandler) workspace_plan.ExpireSubscriptionsUseCase {
	return &expireSubscriptionsUseCase{
		subscriptions: subscriptions,
		onReduced:     onReduced,
		batchSize:     defaultExpireBatchSize,
		graceDays:     expireDunningGraceDays,
		now:           utcNow,
	}
}

func (uc *expireSubscriptionsUseCase) Execute() (int64, error) {
	// Only expire subscriptions overdue beyond the dunning grace, so a monthly subscription unpaid at
	// its anchor keeps serving through dunning and the day-27 sweep owns its cancellation.
	cutoff := uc.now().AddDate(0, 0, -uc.graceDays)
	var total int64
	affected := map[string]struct{}{}
	for {
		workspaceIDs, err := uc.subscriptions.ExpireOverdue(cutoff, uc.batchSize)
		if err != nil {
			uc.reconcileWhatsApp(affected)
			return total, err
		}
		for _, ws := range workspaceIDs {
			affected[ws] = struct{}{}
		}
		total += int64(len(workspaceIDs))
		if len(workspaceIDs) < uc.batchSize {
			break
		}
	}
	uc.reconcileWhatsApp(affected)
	return total, nil
}

// reconcileWhatsApp suspends WhatsApp numbers a lapsed plan was funding. A plan
// expiry drops its IncludedWhatsAppBusinessPhones to 0 in the resolver, so this
// treats the included number exactly like a lapsed addon: OnEntitlementReduced
// cancels (cancellation_request) and suspends any dialog360 number now over the
// effective entitlement. Best-effort and idempotent: a failure is logged and the
// number stays over-cap until the next reconcile pass.
func (uc *expireSubscriptionsUseCase) reconcileWhatsApp(workspaceIDs map[string]struct{}) {
	if uc.onReduced == nil {
		return
	}
	for ws := range workspaceIDs {
		if err := uc.onReduced.OnEntitlementReduced(ws, workspace_addon.EntitlementWhatsAppBusinessPhones); err != nil {
			log.Printf("[expire-subscriptions] whatsapp reconcile failed for workspace %s: %v", ws, err)
		}
	}
}
