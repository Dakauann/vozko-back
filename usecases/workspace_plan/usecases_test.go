package workspace_plan_usecase

import (
	"errors"
	"testing"
	"time"

	billing "vozko/domain/billing"
	"vozko/domain/invoice"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type testPlanRepo struct {
	plans        map[string]*workspace_plan.PlanDefinition
	pricingItems map[string][]workspace_plan.PlanPricingItem
	visibility   map[string][]string
	wsNames      map[string]string
}

func newTestPlanRepo() *testPlanRepo {
	return &testPlanRepo{
		plans:        make(map[string]*workspace_plan.PlanDefinition),
		pricingItems: make(map[string][]workspace_plan.PlanPricingItem),
		visibility:   make(map[string][]string),
		wsNames:      make(map[string]string),
	}
}

func (r *testPlanRepo) Create(plan *workspace_plan.PlanDefinition) error {
	r.plans[plan.ID] = plan
	return nil
}

func (r *testPlanRepo) Update(plan *workspace_plan.PlanDefinition) error {
	if _, ok := r.plans[plan.ID]; !ok {
		return workspace_plan.ErrPlanNotFound
	}
	r.plans[plan.ID] = plan
	return nil
}

func (r *testPlanRepo) Archive(planID string, archivedAt time.Time) error {
	plan, ok := r.plans[planID]
	if !ok {
		return workspace_plan.ErrPlanNotFound
	}
	plan.ArchivedAt = &archivedAt
	plan.UpdatedAt = archivedAt
	return nil
}

func (r *testPlanRepo) GetByID(planID string) (*workspace_plan.PlanDefinition, error) {
	plan, ok := r.plans[planID]
	if !ok {
		return nil, workspace_plan.ErrPlanNotFound
	}
	plan.PricingItems = r.pricingItems[planID]
	return plan, nil
}

func (r *testPlanRepo) List(includeArchived bool) ([]*workspace_plan.PlanDefinition, error) {
	result := make([]*workspace_plan.PlanDefinition, 0, len(r.plans))
	for _, plan := range r.plans {
		if !includeArchived && plan.IsArchived() {
			continue
		}
		plan.PricingItems = r.pricingItems[plan.ID]
		result = append(result, plan)
	}
	return result, nil
}

func (r *testPlanRepo) ReplacePricingItems(planID string, items []workspace_plan.PlanPricingItem) error {
	r.pricingItems[planID] = items
	return nil
}

func (r *testPlanRepo) ListPricingItems(planID string) ([]workspace_plan.PlanPricingItem, error) {
	return r.pricingItems[planID], nil
}

func (r *testPlanRepo) SetVisibility(planID string, workspaceIDs []string) error {
	if workspaceIDs == nil {
		delete(r.visibility, planID)
	} else {
		r.visibility[planID] = workspaceIDs
	}
	return nil
}

func (r *testPlanRepo) GetAllowedWorkspaceIDs(planID string) ([]string, error) {
	ids := r.visibility[planID]
	return ids, nil
}

func (r *testPlanRepo) GetAllowedWorkspaces(planID string) ([]workspace_plan.AllowedWorkspace, error) {
	ids := r.visibility[planID]
	ws := make([]workspace_plan.AllowedWorkspace, len(ids))
	for i, id := range ids {
		name := r.wsNames[id]
		if name == "" {
			name = id
		}
		ws[i] = workspace_plan.AllowedWorkspace{ID: id, Name: name}
	}
	return ws, nil
}

func (r *testPlanRepo) ListVisiblePlans(workspaceID string) ([]*workspace_plan.PlanDefinition, error) {
	result := make([]*workspace_plan.PlanDefinition, 0)
	for _, plan := range r.plans {
		if plan.IsArchived() {
			continue
		}
		if plan.IsExclusive() {
			continue
		}
		if plan.IsGloballyVisible {
			result = append(result, plan)
			continue
		}
		for _, id := range r.visibility[plan.ID] {
			if id == workspaceID {
				result = append(result, plan)
				break
			}
		}
	}
	return result, nil
}

func (r *testPlanRepo) SetExclusiveAffiliate(planID string, affiliateID *string) error {
	plan, ok := r.plans[planID]
	if !ok {
		return workspace_plan.ErrPlanNotFound
	}
	plan.ExclusiveAffiliateID = affiliateID
	return nil
}

func (r *testPlanRepo) ListByExclusiveAffiliateID(affiliateID string) ([]*workspace_plan.PlanDefinition, error) {
	result := make([]*workspace_plan.PlanDefinition, 0)
	for _, plan := range r.plans {
		if plan.IsArchived() || !plan.IsExclusive() {
			continue
		}
		if *plan.ExclusiveAffiliateID == affiliateID {
			plan.PricingItems = r.pricingItems[plan.ID]
			result = append(result, plan)
		}
	}
	return result, nil
}

type testSubscriptionRepo struct {
	subscriptions map[string]*workspace_plan.WorkspaceSubscription
}

type testInvoiceCreator struct {
	lastInput invoice.CreateInvoiceInput
	output    *invoice.CreateInvoiceOutput
	err       error
}

func (c *testInvoiceCreator) Execute(input invoice.CreateInvoiceInput) (*invoice.CreateInvoiceOutput, error) {
	c.lastInput = input
	if c.err != nil {
		return nil, c.err
	}
	if c.output != nil {
		return c.output, nil
	}
	planDefinitionID := input.PlanDefinitionID
	return &invoice.CreateInvoiceOutput{Invoice: &invoice.Invoice{
		ID:               "inv-1",
		WorkspaceID:      input.WorkspaceID,
		UserID:           input.UserID,
		Purpose:          input.Purpose,
		PlanDefinitionID: &planDefinitionID,
		AmountBRL:        input.AmountBRL,
		BillingType:      input.BillingType,
		Description:      input.Description,
	}}, nil
}

func newTestSubscriptionRepo() *testSubscriptionRepo {
	return &testSubscriptionRepo{
		subscriptions: make(map[string]*workspace_plan.WorkspaceSubscription),
	}
}

func (r *testSubscriptionRepo) Create(subscription *workspace_plan.WorkspaceSubscription) error {
	r.subscriptions[subscription.ID] = subscription
	return nil
}

func (r *testSubscriptionRepo) Update(subscription *workspace_plan.WorkspaceSubscription) error {
	if _, ok := r.subscriptions[subscription.ID]; !ok {
		return workspace_plan.ErrSubscriptionNotFound
	}
	r.subscriptions[subscription.ID] = subscription
	return nil
}

func (r *testSubscriptionRepo) GetCurrentByWorkspaceID(workspaceID string, at time.Time) (*workspace_plan.WorkspaceSubscription, error) {
	latest, err := r.GetLatestByWorkspaceID(workspaceID)
	if err != nil {
		if errors.Is(err, workspace_plan.ErrSubscriptionNotFound) {
			return nil, workspace_plan.ErrSubscriptionNotCurrent
		}
		return nil, err
	}
	if latest.IsCurrent(at) {
		return latest, nil
	}
	return nil, workspace_plan.ErrSubscriptionNotCurrent
}

func (r *testSubscriptionRepo) GetLatestByWorkspaceID(workspaceID string) (*workspace_plan.WorkspaceSubscription, error) {
	var latest *workspace_plan.WorkspaceSubscription
	for _, subscription := range r.subscriptions {
		if subscription.WorkspaceID != workspaceID {
			continue
		}
		if latest == nil || subscription.CurrentPeriodEnd.After(latest.CurrentPeriodEnd) {
			latest = subscription
		}
	}
	if latest == nil {
		return nil, workspace_plan.ErrSubscriptionNotFound
	}
	return latest, nil
}

func (r *testSubscriptionRepo) GetCurrentByWorkspaceIDs(workspaceIDs []string, at time.Time) (map[string]*workspace_plan.WorkspaceSubscription, error) {
	result := make(map[string]*workspace_plan.WorkspaceSubscription)
	for _, sub := range r.subscriptions {
		for _, id := range workspaceIDs {
			if sub.WorkspaceID == id && sub.IsCurrent(at) {
				result[id] = sub
			}
		}
	}
	return result, nil
}

func (r *testSubscriptionRepo) ExpireOverdue(at time.Time, batchSize int) ([]string, error) {
	var workspaceIDs []string
	for _, sub := range r.subscriptions {
		if len(workspaceIDs) >= batchSize {
			break
		}
		if (sub.Status == workspace_plan.SubscriptionStatusActive || sub.Status == workspace_plan.SubscriptionStatusCancelled) && at.After(sub.CurrentPeriodEnd) {
			sub.Status = workspace_plan.SubscriptionStatusExpired
			sub.UpdatedAt = at
			workspaceIDs = append(workspaceIDs, sub.WorkspaceID)
		}
	}
	return workspaceIDs, nil
}
func (r *testSubscriptionRepo) ListUpcomingExpirations(time.Time, time.Time, int) ([]*workspace_plan.WorkspaceSubscription, error) {
	return nil, nil
}
func (r *testSubscriptionRepo) ListActiveBillingDue(time.Time, string, int) ([]*workspace_plan.WorkspaceSubscription, error) {
	return nil, nil
}

func TestCreatePlanDefinitionUseCase_CreatesPlanAndNormalizesPricing(t *testing.T) {
	repo := newTestPlanRepo()
	uc := NewCreatePlanDefinitionUseCase(repo)

	plan, err := uc.Execute(workspace_plan.PlanMutationInput{
		Name:              "Growth",
		Description:       "Growth plan",
		BasePriceBRLCents: 29900,
		MaxCallChannels:   7,
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if plan == nil || plan.Name != "Growth" {
		t.Fatalf("expected created plan with name Growth, got %+v", plan)
	}
	if plan.MaxCallChannels != 7 {
		t.Fatalf("expected max call channels 7, got %d", plan.MaxCallChannels)
	}
	if plan.BasePriceBRLCents != 29900 {
		t.Fatalf("expected base price 29900, got %d", plan.BasePriceBRLCents)
	}
}

func TestSubscribeWorkspaceUseCase_CreatesSubscriptionAndBlocksDuplicate(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Starter",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
	})

	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subscriptions, now: func() time.Time { return fixedNow }}
	details, err := uc.Execute("ws-1", "plan-1", workspace_plan.BillingCycleMonthly)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if details.Subscription.PlanDefinitionID != "plan-1" {
		t.Fatalf("expected plan definition ID plan-1, got %s", details.Subscription.PlanDefinitionID)
	}
	if details.Plan == nil || details.Plan.Name != "Starter" {
		t.Fatalf("expected plan Starter in details, got %+v", details.Plan)
	}

	_, err = uc.Execute("ws-1", "plan-1", workspace_plan.BillingCycleMonthly)
	if !errors.Is(err, workspace_plan.ErrSubscriptionAlreadyExists) {
		t.Fatalf("expected ErrSubscriptionAlreadyExists, got %v", err)
	}
}

func TestCreateSubscriptionInvoiceUseCase_CreatesInvoiceAndBlocksDuplicateCurrent(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	invoices := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Starter",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
		IsGloballyVisible: true,
	})

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subscriptions, createInvoice: invoices, now: func() time.Time { return fixedNow }}
	output, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if output.Invoice == nil || output.Invoice.NormalizedPurpose() != invoice.PurposeSubscription {
		t.Fatalf("expected subscription invoice output, got %+v", output.Invoice)
	}
	if invoices.lastInput.AmountBRL != 199 {
		t.Fatalf("expected base price invoice of 199 BRL, got %.2f", invoices.lastInput.AmountBRL)
	}
	if invoices.lastInput.PlanDefinitionID != "plan-1" {
		t.Fatalf("expected plan ID plan-1, got %s", invoices.lastInput.PlanDefinitionID)
	}
	if invoices.lastInput.Description != "Assinatura do plano Starter" {
		t.Fatalf("expected default description to include plan name, got %q", invoices.lastInput.Description)
	}

	subscriptions.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-time.Hour),
		CurrentPeriodEnd:   fixedNow.Add(time.Hour),
	}

	_, err = uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX"})
	if !errors.Is(err, workspace_plan.ErrSubscriptionAlreadyExists) {
		t.Fatalf("expected ErrSubscriptionAlreadyExists, got %v", err)
	}
}

func TestCreateSubscriptionInvoiceUseCase_AllowsCancelledCurrentSubscription(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	invoices := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Starter",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
		IsGloballyVisible: true,
	})
	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-2",
		Name:              "Pro",
		BasePriceBRLCents: 70000,
		MaxCallChannels:   6,
		IsGloballyVisible: true,
	})

	cancelledAt := fixedNow.Add(-30 * time.Minute)
	subscriptions.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusCancelled,
		CurrentPeriodStart: fixedNow.Add(-24 * time.Hour),
		CurrentPeriodEnd:   fixedNow.Add(24 * time.Hour),
		CancelledAt:        &cancelledAt,
	}

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subscriptions, createInvoice: invoices, now: func() time.Time { return fixedNow }}
	output, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-2", BillingType: "PIX"})
	if err != nil {
		t.Fatalf("cancelled subscription should allow re-contracting, got error: %v", err)
	}
	if output.Invoice == nil {
		t.Fatal("expected invoice to be created")
	}
	if invoices.lastInput.PlanDefinitionID != "plan-2" {
		t.Fatalf("expected invoice for plan-2, got %s", invoices.lastInput.PlanDefinitionID)
	}
	if invoices.lastInput.AmountBRL != 700 {
		t.Fatalf("expected Pro price 700 BRL, got %.2f", invoices.lastInput.AmountBRL)
	}
}

func TestCreateSubscriptionInvoiceUseCase_BlocksActiveCurrentSubscription(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	invoices := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Starter",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
		IsGloballyVisible: true,
	})

	subscriptions.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-24 * time.Hour),
		CurrentPeriodEnd:   fixedNow.Add(24 * time.Hour),
	}

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subscriptions, createInvoice: invoices, now: func() time.Time { return fixedNow }}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX"})
	if !errors.Is(err, workspace_plan.ErrSubscriptionAlreadyExists) {
		t.Fatalf("expected ErrSubscriptionAlreadyExists for active current subscription, got %v", err)
	}
	if invoices.lastInput.PlanDefinitionID != "" {
		t.Fatalf("expected invoice creator not to be called, got plan %q", invoices.lastInput.PlanDefinitionID)
	}
}

func TestCreateSubscriptionInvoiceUseCase_AllowsExpiredSubscription(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	invoices := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Starter",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
		IsGloballyVisible: true,
	})

	subscriptions.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusExpired,
		CurrentPeriodStart: fixedNow.AddDate(0, -2, 0),
		CurrentPeriodEnd:   fixedNow.AddDate(0, -1, 0),
	}

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subscriptions, createInvoice: invoices, now: func() time.Time { return fixedNow }}
	output, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX"})
	if err != nil {
		t.Fatalf("expired subscription should allow new invoice, got error: %v", err)
	}
	if output.Invoice == nil {
		t.Fatal("expected invoice to be created")
	}
}

func TestCreateSubscriptionInvoiceUseCase_CancelledSamePlanRecontract(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	invoices := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Starter",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
		IsGloballyVisible: true,
	})

	cancelledAt := fixedNow.Add(-time.Hour)
	subscriptions.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusCancelled,
		CurrentPeriodStart: fixedNow.Add(-24 * time.Hour),
		CurrentPeriodEnd:   fixedNow.Add(24 * time.Hour),
		CancelledAt:        &cancelledAt,
	}

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subscriptions, createInvoice: invoices, now: func() time.Time { return fixedNow }}
	output, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX"})
	if err != nil {
		t.Fatalf("cancelled subscription should allow re-contracting same plan, got error: %v", err)
	}
	if output.Invoice == nil {
		t.Fatal("expected invoice to be created")
	}
	if invoices.lastInput.PlanDefinitionID != "plan-1" {
		t.Fatalf("expected invoice for plan-1, got %s", invoices.lastInput.PlanDefinitionID)
	}
}

func TestEnsureCurrentWorkspaceSubscriptionUseCase_ExpiresPastDueSubscription(t *testing.T) {
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	subscription := &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.AddDate(0, -2, 0),
		CurrentPeriodEnd:   fixedNow.AddDate(0, -1, 0),
	}
	subscriptions.subscriptions[subscription.ID] = subscription

	uc := &ensureCurrentWorkspaceSubscriptionUseCase{subscriptions: subscriptions, now: func() time.Time { return fixedNow }}
	_, err := uc.Execute("ws-1")
	if !errors.Is(err, workspace_plan.ErrSubscriptionNotCurrent) {
		t.Fatalf("expected ErrSubscriptionNotCurrent, got %v", err)
	}
	if subscription.Status != workspace_plan.SubscriptionStatusExpired {
		t.Fatalf("expected subscription status expired, got %s", subscription.Status)
	}
}

func TestRenewWorkspaceSubscriptionUseCase_ReactivatesExpiredSubscription(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Starter",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
	})

	subscription := &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusExpired,
		CurrentPeriodStart: fixedNow.AddDate(0, -2, 0),
		CurrentPeriodEnd:   fixedNow.AddDate(0, -1, 0),
	}
	subscriptions.subscriptions[subscription.ID] = subscription

	uc := &renewWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subscriptions, now: func() time.Time { return fixedNow }}
	details, err := uc.Execute("ws-1")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if details.Subscription.Status != workspace_plan.SubscriptionStatusActive {
		t.Fatalf("expected active status after renewal, got %s", details.Subscription.Status)
	}
	if !details.Subscription.CurrentPeriodEnd.After(fixedNow) {
		t.Fatal("expected renewed period end after current time")
	}
	if details.Plan == nil || details.Plan.Name != "Starter" {
		t.Fatalf("expected plan Starter in details, got %+v", details.Plan)
	}
}

func TestCreatePlanDefinitionUseCase_SeedsFullCatalog(t *testing.T) {
	repo := newTestPlanRepo()
	uc := NewCreatePlanDefinitionUseCase(repo)

	catalogSize := len(workspace_pricing.DefaultPricingCatalog) - 1

	plan, err := uc.Execute(workspace_plan.PlanMutationInput{
		Name:              "Pro",
		Description:       "Pro plan",
		BasePriceBRLCents: 99900,
		MaxCallChannels:   10,
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if len(plan.PricingItems) != catalogSize {
		t.Fatalf("expected %d pricing items (full catalog), got %d", catalogSize, len(plan.PricingItems))
	}
	for _, item := range plan.PricingItems {
		if item.PlanDefinitionID != plan.ID {
			t.Errorf("expected planID %s, got %s", plan.ID, item.PlanDefinitionID)
		}
		if item.ID == "" {
			t.Error("expected non-empty item ID")
		}
	}
}

func TestCreatePlanDefinitionUseCase_OverridesAppliedOnCreate(t *testing.T) {
	repo := newTestPlanRepo()
	uc := NewCreatePlanDefinitionUseCase(repo)

	catalogSize := len(workspace_pricing.DefaultPricingCatalog) - 1

	plan, err := uc.Execute(workspace_plan.PlanMutationInput{
		Name:              "Pro",
		Description:       "Pro plan with custom WA pricing",
		BasePriceBRLCents: 99900,
		MaxCallChannels:   10,
		PricingItems: []workspace_plan.PlanPricingItemInput{
			{Category: "whatsapp", Service: "utility", Metric: "per_message", PriceMicros: 25_000, CostMicros: 5_000, Currency: "USD"},
		},
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if len(plan.PricingItems) != catalogSize {
		t.Fatalf("expected %d items, got %d", catalogSize, len(plan.PricingItems))
	}

	var waUtility *workspace_plan.PlanPricingItem
	for i := range plan.PricingItems {
		if plan.PricingItems[i].Category == "whatsapp" && plan.PricingItems[i].Service == "utility" {
			waUtility = &plan.PricingItems[i]
			break
		}
	}
	if waUtility == nil {
		t.Fatal("missing whatsapp|utility item")
	}
	if waUtility.PriceMicros != 25_000 {
		t.Errorf("expected overridden price 25000, got %d", waUtility.PriceMicros)
	}
	if waUtility.CostMicros != 5_000 {
		t.Errorf("expected overridden cost 5000, got %d", waUtility.CostMicros)
	}
}

func TestUpdatePlanDefinitionUseCase_OverridesPreservedOnUpdate(t *testing.T) {
	repo := newTestPlanRepo()
	catalogSize := len(workspace_pricing.DefaultPricingCatalog) - 1

	createUC := NewCreatePlanDefinitionUseCase(repo)
	plan, _ := createUC.Execute(workspace_plan.PlanMutationInput{
		Name:              "Starter",
		BasePriceBRLCents: 49900,
		MaxCallChannels:   3,
		PricingItems: []workspace_plan.PlanPricingItemInput{
			{Category: "whatsapp", Service: "utility", Metric: "per_message", PriceMicros: 20_000, Currency: "USD"},
		},
	})

	updateUC := NewUpdatePlanDefinitionUseCase(repo)
	updated, err := updateUC.Execute(plan.ID, workspace_plan.PlanMutationInput{
		Name:              "Starter Plus",
		BasePriceBRLCents: 59900,
		MaxCallChannels:   5,
		PricingItems: []workspace_plan.PlanPricingItemInput{
			{Category: "whatsapp", Service: "utility", Metric: "per_message", PriceMicros: 18_000, Currency: "USD"},
			{Category: "sms", Service: "standard", Metric: "per_message", PriceMicros: 8_000, Currency: "USD"},
		},
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if len(updated.PricingItems) != catalogSize {
		t.Fatalf("expected %d items, got %d", catalogSize, len(updated.PricingItems))
	}

	for _, item := range updated.PricingItems {
		if item.Category == "whatsapp" && item.Service == "utility" {
			if item.PriceMicros != 18_000 {
				t.Errorf("expected WA price 18000, got %d", item.PriceMicros)
			}
		}
		if item.Category == "sms" && item.Service == "standard" {
			if item.PriceMicros != 8_000 {
				t.Errorf("expected SMS price 8000, got %d", item.PriceMicros)
			}
		}
	}
}

func TestCreatePlanDefinitionUseCase_NoPricingInputStillSeedsFull(t *testing.T) {
	repo := newTestPlanRepo()
	uc := NewCreatePlanDefinitionUseCase(repo)

	catalogSize := len(workspace_pricing.DefaultPricingCatalog) - 1

	plan, err := uc.Execute(workspace_plan.PlanMutationInput{
		Name:              "Basic",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   2,
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if len(plan.PricingItems) != catalogSize {
		t.Fatalf("expected %d pricing items, got %d", catalogSize, len(plan.PricingItems))
	}
}

func TestGetPlanDefinitionUseCase_MergesNewCatalogEntries(t *testing.T) {
	repo := newTestPlanRepo()

	createUC := NewCreatePlanDefinitionUseCase(repo)
	plan, _ := createUC.Execute(workspace_plan.PlanMutationInput{
		Name:              "Standard",
		BasePriceBRLCents: 29900,
		MaxCallChannels:   3,
	})

	getUC := NewGetPlanDefinitionUseCase(repo)
	fetched, err := getUC.Execute(plan.ID)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	catalogSize := len(workspace_pricing.DefaultPricingCatalog) - 1
	if len(fetched.PricingItems) != catalogSize {
		t.Fatalf("expected %d items on get, got %d", catalogSize, len(fetched.PricingItems))
	}
}

func TestListPlanDefinitionsUseCase_MergesNewCatalogEntries(t *testing.T) {
	repo := newTestPlanRepo()
	catalogSize := len(workspace_pricing.DefaultPricingCatalog) - 1

	createUC := NewCreatePlanDefinitionUseCase(repo)
	createUC.Execute(workspace_plan.PlanMutationInput{
		Name:              "Plan A",
		BasePriceBRLCents: 10000,
		MaxCallChannels:   2,
	})
	createUC.Execute(workspace_plan.PlanMutationInput{
		Name:              "Plan B",
		BasePriceBRLCents: 20000,
		MaxCallChannels:   5,
	})

	listUC := NewListPlanDefinitionsUseCase(repo)
	plans, err := listUC.Execute(false)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(plans))
	}
	for _, p := range plans {
		if len(p.PricingItems) != catalogSize {
			t.Errorf("plan %s: expected %d items, got %d", p.Name, catalogSize, len(p.PricingItems))
		}
	}
}

func TestSubscribeWorkspaceUseCase_EmptyWorkspaceID(t *testing.T) {
	uc := &subscribeWorkspaceUseCase{now: fixedClock(time.Now())}
	_, err := uc.Execute("", "plan-1", workspace_plan.BillingCycleMonthly)
	if !errors.Is(err, workspace_plan.ErrInvalidSubscription) {
		t.Fatalf("expected ErrInvalidSubscription, got %v", err)
	}
}

func TestSubscribeWorkspaceUseCase_EmptyPlanID(t *testing.T) {
	uc := &subscribeWorkspaceUseCase{now: fixedClock(time.Now())}
	_, err := uc.Execute("ws-1", "", workspace_plan.BillingCycleMonthly)
	if !errors.Is(err, workspace_plan.ErrInvalidSubscription) {
		t.Fatalf("expected ErrInvalidSubscription, got %v", err)
	}
}

func TestSubscribeWorkspaceUseCase_ArchivedPlanRejected(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	archived := fixedNow.Add(-24 * time.Hour)
	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-archived",
		Name:              "Old Plan",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
		ArchivedAt:        &archived,
	})

	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subscriptions, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "plan-archived", workspace_plan.BillingCycleMonthly)
	if !errors.Is(err, workspace_plan.ErrPlanArchived) {
		t.Fatalf("expected ErrPlanArchived, got %v", err)
	}
}

func TestSubscribeWorkspaceUseCase_PlanNotFound(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subscriptions, now: fixedClock(time.Now())}
	_, err := uc.Execute("ws-1", "nonexistent", workspace_plan.BillingCycleMonthly)
	if !errors.Is(err, workspace_plan.ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestSubscribeWorkspaceUseCase_CopiesPlanNameAndMaxChannels(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-pro",
		Name:              "Pro",
		BasePriceBRLCents: 49900,
		MaxCallChannels:   10,
	})

	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subscriptions, now: fixedClock(fixedNow)}
	details, err := uc.Execute("ws-1", "plan-pro", workspace_plan.BillingCycleMonthly)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if details.Subscription.PlanName != "Pro" {
		t.Fatalf("expected PlanName Pro, got %q", details.Subscription.PlanName)
	}
	if details.Subscription.MaxCallChannels != 10 {
		t.Fatalf("expected MaxCallChannels 10, got %d", details.Subscription.MaxCallChannels)
	}
}

func TestSubscribeWorkspaceUseCase_ExpiresOldBeforeCreating(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Starter",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
	})

	subscriptions.subscriptions["sub-old"] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-old",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.AddDate(0, -3, 0),
		CurrentPeriodEnd:   fixedNow.AddDate(0, -2, 0),
	}

	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subscriptions, now: fixedClock(fixedNow)}
	details, err := uc.Execute("ws-1", "plan-1", workspace_plan.BillingCycleMonthly)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if details.Subscription.Status != workspace_plan.SubscriptionStatusActive {
		t.Fatalf("expected active, got %s", details.Subscription.Status)
	}

	old := subscriptions.subscriptions["sub-old"]
	if old.Status != workspace_plan.SubscriptionStatusExpired {
		t.Fatalf("expected old sub expired, got %s", old.Status)
	}
}

func TestCreateSubscriptionInvoiceUseCase_EmptyInputs(t *testing.T) {
	uc := &createSubscriptionInvoiceUseCase{now: fixedClock(time.Now())}
	_, err := uc.Execute("", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "p"})
	if !errors.Is(err, workspace_plan.ErrInvalidSubscription) {
		t.Fatalf("expected ErrInvalidSubscription for empty workspace, got %v", err)
	}
	_, err = uc.Execute("ws-1", "", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "p"})
	if !errors.Is(err, workspace_plan.ErrInvalidSubscription) {
		t.Fatalf("expected ErrInvalidSubscription for empty user, got %v", err)
	}
	_, err = uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: ""})
	if !errors.Is(err, workspace_plan.ErrInvalidSubscription) {
		t.Fatalf("expected ErrInvalidSubscription for empty plan, got %v", err)
	}
}

func TestCreateSubscriptionInvoiceUseCase_ArchivedPlan(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	invoices := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	archived := fixedNow.Add(-time.Hour)
	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-archived",
		Name:              "Old",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
		ArchivedAt:        &archived,
	})

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subscriptions, createInvoice: invoices, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-archived", BillingType: "PIX"})
	if !errors.Is(err, workspace_plan.ErrPlanArchived) {
		t.Fatalf("expected ErrPlanArchived, got %v", err)
	}
}

func TestCreateSubscriptionInvoiceUseCase_ZeroPriceRejected(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	invoices := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-free",
		Name:              "Free",
		BasePriceBRLCents: 0,
		MaxCallChannels:   1,
		IsGloballyVisible: true,
	})

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subscriptions, createInvoice: invoices, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-free", BillingType: "PIX"})
	if !errors.Is(err, invoice.ErrInvalidAmount) {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}
}

func TestCreateSubscriptionInvoiceUseCase_CustomDescription(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	invoices := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Starter",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
		IsGloballyVisible: true,
	})

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subscriptions, createInvoice: invoices, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{
		PlanID:      "plan-1",
		BillingType: "BOLETO",
		Description: "Custom desc",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if invoices.lastInput.Description != "Custom desc" {
		t.Fatalf("expected custom description, got %q", invoices.lastInput.Description)
	}
	if invoices.lastInput.BillingType != "BOLETO" {
		t.Fatalf("expected billing type BOLETO, got %q", invoices.lastInput.BillingType)
	}
}

func TestCancelWorkspaceSubscriptionUseCase_CancelsActive(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Starter",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
	})

	subscriptions.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-time.Hour),
		CurrentPeriodEnd:   fixedNow.Add(24 * time.Hour),
	}

	uc := &cancelWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subscriptions, now: fixedClock(fixedNow)}
	details, err := uc.Execute("ws-1")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if details.Subscription.Status != workspace_plan.SubscriptionStatusCancelled {
		t.Fatalf("expected cancelled, got %s", details.Subscription.Status)
	}
	if details.Subscription.CancelledAt == nil {
		t.Fatal("expected CancelledAt to be set")
	}
	if *details.Subscription.CancelledAt != fixedNow {
		t.Fatalf("expected CancelledAt = fixedNow, got %v", *details.Subscription.CancelledAt)
	}
	if details.Plan == nil || details.Plan.Name != "Starter" {
		t.Fatalf("expected plan Starter, got %+v", details.Plan)
	}
}

func TestCancelWorkspaceSubscriptionUseCase_AlreadyCancelled(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Starter",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
	})

	cancelledAt := fixedNow.Add(-time.Hour)
	subscriptions.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusCancelled,
		CurrentPeriodStart: fixedNow.Add(-48 * time.Hour),
		CurrentPeriodEnd:   fixedNow.Add(24 * time.Hour),
		CancelledAt:        &cancelledAt,
	}

	uc := &cancelWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subscriptions, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1")
	if !errors.Is(err, workspace_plan.ErrSubscriptionCancelled) {
		t.Fatalf("expected ErrSubscriptionCancelled, got %v", err)
	}
}

func TestCancelWorkspaceSubscriptionUseCase_NoSubscription(t *testing.T) {
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	uc := &cancelWorkspaceSubscriptionUseCase{subscriptions: subscriptions, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1")
	if !errors.Is(err, workspace_plan.ErrSubscriptionNotCurrent) {
		t.Fatalf("expected ErrSubscriptionNotCurrent, got %v", err)
	}
}

func TestGetWorkspaceSubscriptionUseCase_ReturnsActiveWithPlan(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Starter",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
	})

	subscriptions.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-time.Hour),
		CurrentPeriodEnd:   fixedNow.Add(24 * time.Hour),
	}

	uc := &getWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subscriptions, now: fixedClock(fixedNow)}
	details, err := uc.Execute("ws-1")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if details.Subscription.ID != "sub-1" {
		t.Fatalf("expected sub-1, got %s", details.Subscription.ID)
	}
	if details.Plan.Name != "Starter" {
		t.Fatalf("expected plan Starter, got %s", details.Plan.Name)
	}
}

func TestGetWorkspaceSubscriptionUseCase_ExpiresOld(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Starter",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
	})

	subscriptions.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.AddDate(0, -2, 0),
		CurrentPeriodEnd:   fixedNow.AddDate(0, -1, 0),
	}

	uc := &getWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subscriptions, now: fixedClock(fixedNow)}
	details, err := uc.Execute("ws-1")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if details.Subscription.Status != workspace_plan.SubscriptionStatusExpired {
		t.Fatalf("expected expired, got %s", details.Subscription.Status)
	}
}

func TestGetWorkspaceSubscriptionUseCase_NoSubscription(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	uc := &getWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subscriptions, now: fixedClock(time.Now())}
	_, err := uc.Execute("ws-1")
	if !errors.Is(err, workspace_plan.ErrSubscriptionNotFound) {
		t.Fatalf("expected ErrSubscriptionNotFound, got %v", err)
	}
}

func TestRenewWorkspaceSubscriptionUseCase_RefreshesPlanNameAndChannels(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Starter V2",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   5,
	})

	subscriptions.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusExpired,
		CurrentPeriodStart: fixedNow.AddDate(0, -2, 0),
		CurrentPeriodEnd:   fixedNow.AddDate(0, -1, 0),
	}

	uc := &renewWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subscriptions, now: fixedClock(fixedNow)}
	details, err := uc.Execute("ws-1")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if details.Subscription.PlanName != "Starter V2" {
		t.Fatalf("expected PlanName 'Starter V2', got %q", details.Subscription.PlanName)
	}
	if details.Subscription.MaxCallChannels != 5 {
		t.Fatalf("expected MaxCallChannels 5, got %d", details.Subscription.MaxCallChannels)
	}
}

func TestRenewWorkspaceSubscriptionUseCase_AlreadyCurrent(t *testing.T) {
	plans := newTestPlanRepo()
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Starter",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
	})

	subscriptions.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-time.Hour),
		CurrentPeriodEnd:   fixedNow.Add(24 * time.Hour),
	}

	uc := &renewWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subscriptions, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1")
	if !errors.Is(err, workspace_plan.ErrSubscriptionAlreadyExists) {
		t.Fatalf("expected ErrSubscriptionAlreadyExists, got %v", err)
	}
}

func TestRenewWorkspaceSubscriptionUseCase_NoSubscription(t *testing.T) {
	subscriptions := newTestSubscriptionRepo()
	uc := &renewWorkspaceSubscriptionUseCase{subscriptions: subscriptions, now: fixedClock(time.Now())}
	_, err := uc.Execute("ws-1")
	if !errors.Is(err, workspace_plan.ErrSubscriptionNotFound) {
		t.Fatalf("expected ErrSubscriptionNotFound, got %v", err)
	}
}

func TestEnsureCurrentWorkspaceSubscriptionUseCase_ReturnsCurrentActive(t *testing.T) {
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	subscriptions.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-time.Hour),
		CurrentPeriodEnd:   fixedNow.Add(24 * time.Hour),
	}

	uc := &ensureCurrentWorkspaceSubscriptionUseCase{subscriptions: subscriptions, now: fixedClock(fixedNow)}
	sub, err := uc.Execute("ws-1")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if sub.ID != "sub-1" {
		t.Fatalf("expected sub-1, got %s", sub.ID)
	}
}

func TestEnsureCurrentWorkspaceSubscriptionUseCase_NoSubscription(t *testing.T) {
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	uc := &ensureCurrentWorkspaceSubscriptionUseCase{subscriptions: subscriptions, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1")
	if !errors.Is(err, workspace_plan.ErrSubscriptionNotCurrent) {
		t.Fatalf("expected ErrSubscriptionNotCurrent, got %v", err)
	}
}

func TestEnsureActiveWorkspaceSubscriptionUseCase_ReturnsCurrentActive(t *testing.T) {
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	subscriptions.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-time.Hour),
		CurrentPeriodEnd:   fixedNow.Add(24 * time.Hour),
	}

	currentUC := &ensureCurrentWorkspaceSubscriptionUseCase{subscriptions: subscriptions, now: fixedClock(fixedNow)}
	uc := NewEnsureActiveWorkspaceSubscriptionUseCase(currentUC)
	sub, err := uc.Execute("ws-1")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if sub.ID != "sub-1" {
		t.Fatalf("expected sub-1, got %s", sub.ID)
	}
}

func TestEnsureActiveWorkspaceSubscriptionUseCase_CancelledCurrentSubscription(t *testing.T) {
	subscriptions := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	subscriptions.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-1",
		WorkspaceID:        "ws-1",
		PlanDefinitionID:   "plan-1",
		PlanName:           "Starter",
		MaxCallChannels:    3,
		Status:             workspace_plan.SubscriptionStatusCancelled,
		CurrentPeriodStart: fixedNow.Add(-time.Hour),
		CurrentPeriodEnd:   fixedNow.Add(24 * time.Hour),
	}

	currentUC := &ensureCurrentWorkspaceSubscriptionUseCase{subscriptions: subscriptions, now: fixedClock(fixedNow)}
	uc := NewEnsureActiveWorkspaceSubscriptionUseCase(currentUC)
	_, err := uc.Execute("ws-1")
	if !errors.Is(err, workspace_plan.ErrSubscriptionNotActive) {
		t.Fatalf("expected ErrSubscriptionNotActive, got %v", err)
	}
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

type mockSubReader struct {
	sub *workspace_plan.WorkspaceSubscription
	err error
}

func (m *mockSubReader) GetCurrentByWorkspaceID(workspaceID string, at time.Time) (*workspace_plan.WorkspaceSubscription, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.sub, nil
}

func TestPlanPricingAdapter_ListForWorkspace_Success(t *testing.T) {
	plans := newTestPlanRepo()
	plans.Create(&workspace_plan.PlanDefinition{ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3})
	plans.pricingItems["plan-1"] = []workspace_plan.PlanPricingItem{
		{ID: "i1", PlanDefinitionID: "plan-1", Category: "whatsapp", Service: "utility", Metric: "per_message", PriceMicros: 20_000, CostMicros: 5_000, Currency: "USD"},
		{ID: "i2", PlanDefinitionID: "plan-1", Category: "sms", Service: "standard", Metric: "per_message", PriceMicros: 10_000, CostMicros: 3_000, Currency: "USD"},
	}

	subReader := &mockSubReader{sub: &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-1",
	}}

	adapter := NewPlanPricingAdapter(subReader, plans)
	items, err := adapter.ListForWorkspace("ws-1")
	if err != nil {
		t.Fatalf("ListForWorkspace() error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Category != "whatsapp" || items[0].PriceMicros != 20_000 {
		t.Errorf("item[0] = %+v", items[0])
	}
	if items[1].Category != "sms" || items[1].PriceMicros != 10_000 {
		t.Errorf("item[1] = %+v", items[1])
	}
}

func TestPlanPricingAdapter_ListForWorkspace_NoSubscription(t *testing.T) {
	plans := newTestPlanRepo()
	subReader := &mockSubReader{err: workspace_plan.ErrSubscriptionNotCurrent}

	adapter := NewPlanPricingAdapter(subReader, plans)
	items, err := adapter.ListForWorkspace("ws-1")
	if err != nil {
		t.Fatalf("expected nil error for no subscription, got: %v", err)
	}
	if items != nil {
		t.Fatalf("expected nil items, got %v", items)
	}
}

func TestPlanPricingAdapter_ListForWorkspace_PlanRepoError(t *testing.T) {

	base := newTestPlanRepo()
	plans := &errPricingItemsRepo{testPlanRepo: *base, listPricingErr: errors.New("db error")}
	subReader := &mockSubReader{sub: &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-1",
	}}

	adapter := NewPlanPricingAdapter(subReader, plans)
	_, err := adapter.ListForWorkspace("ws-1")
	if err == nil {
		t.Fatal("expected error when ListPricingItems fails")
	}
}

func TestArchivePlanDefinitionUseCase_Success(t *testing.T) {
	repo := newTestPlanRepo()
	repo.Create(&workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "To Archive",
		BasePriceBRLCents: 19900,
		MaxCallChannels:   3,
	})

	uc := NewArchivePlanDefinitionUseCase(repo)
	err := uc.Execute("plan-1")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	plan := repo.plans["plan-1"]
	if plan.ArchivedAt == nil {
		t.Fatal("expected ArchivedAt to be set")
	}
}

func TestArchivePlanDefinitionUseCase_NotFound(t *testing.T) {
	repo := newTestPlanRepo()
	uc := NewArchivePlanDefinitionUseCase(repo)
	err := uc.Execute("nonexistent")
	if !errors.Is(err, workspace_plan.ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestConstructors_CreateObjects(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	inv := &testInvoiceCreator{}

	if uc := NewCreateSubscriptionInvoiceUseCase(plans, subs, inv, nil); uc == nil {
		t.Fatal("NewCreateSubscriptionInvoiceUseCase returned nil")
	}
	if uc := NewSubscribeWorkspaceUseCase(plans, subs); uc == nil {
		t.Fatal("NewSubscribeWorkspaceUseCase returned nil")
	}
	if uc := NewRenewWorkspaceSubscriptionUseCase(plans, subs); uc == nil {
		t.Fatal("NewRenewWorkspaceSubscriptionUseCase returned nil")
	}
	if uc := NewCancelWorkspaceSubscriptionUseCase(plans, subs); uc == nil {
		t.Fatal("NewCancelWorkspaceSubscriptionUseCase returned nil")
	}
	if uc := NewGetWorkspaceSubscriptionUseCase(plans, subs); uc == nil {
		t.Fatal("NewGetWorkspaceSubscriptionUseCase returned nil")
	}
	if uc := NewEnsureCurrentWorkspaceSubscriptionUseCase(subs); uc == nil {
		t.Fatal("NewEnsureCurrentWorkspaceSubscriptionUseCase returned nil")
	}
}

func TestCreatePlanDefinitionUseCase_RepoCreateError(t *testing.T) {
	base := newTestPlanRepo()
	repo := &errTestPlanRepo{testPlanRepo: *base, createErr: errors.New("constraint violation")}
	uc := &createPlanDefinitionUseCase{repo: repo}

	_, err := uc.Execute(workspace_plan.PlanMutationInput{
		Name: "Test", BasePriceBRLCents: 100, MaxCallChannels: 1,
	})
	if err == nil {
		t.Fatal("expected error when repo.Create fails")
	}
}

func TestCreatePlanDefinitionUseCase_RepoReplacePricingError(t *testing.T) {
	base := newTestPlanRepo()
	repo := &errTestPlanRepo{testPlanRepo: *base, replacePricingErr: errors.New("db error")}
	uc := &createPlanDefinitionUseCase{repo: repo}

	_, err := uc.Execute(workspace_plan.PlanMutationInput{
		Name: "Test", BasePriceBRLCents: 100, MaxCallChannels: 1,
	})
	if err == nil {
		t.Fatal("expected error when ReplacePricingItems fails")
	}
}

func TestUpdatePlanDefinitionUseCase_PlanNotFound(t *testing.T) {
	repo := newTestPlanRepo()
	uc := NewUpdatePlanDefinitionUseCase(repo)
	_, err := uc.Execute("nonexistent", workspace_plan.PlanMutationInput{Name: "X", BasePriceBRLCents: 100, MaxCallChannels: 1})
	if !errors.Is(err, workspace_plan.ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestUpdatePlanDefinitionUseCase_RepoUpdateError(t *testing.T) {
	repo := newTestPlanRepo()
	createUC := NewCreatePlanDefinitionUseCase(repo)
	plan, _ := createUC.Execute(workspace_plan.PlanMutationInput{
		Name: "Test", BasePriceBRLCents: 100, MaxCallChannels: 1,
	})

	errRepo := &errTestPlanRepo{
		testPlanRepo: *repo,
		updateErr:    errors.New("db error"),
	}
	uc := &updatePlanDefinitionUseCase{repo: errRepo}
	_, err := uc.Execute(plan.ID, workspace_plan.PlanMutationInput{Name: "Y", BasePriceBRLCents: 200, MaxCallChannels: 2})
	if err == nil {
		t.Fatal("expected error when repo.Update fails")
	}
}

func TestUpdatePlanDefinitionUseCase_ReplacePricingError(t *testing.T) {
	repo := newTestPlanRepo()
	createUC := NewCreatePlanDefinitionUseCase(repo)
	plan, _ := createUC.Execute(workspace_plan.PlanMutationInput{
		Name: "Test", BasePriceBRLCents: 100, MaxCallChannels: 1,
	})

	errRepo := &errTestPlanRepo{
		testPlanRepo:      *repo,
		replacePricingErr: errors.New("db error"),
	}
	uc := &updatePlanDefinitionUseCase{repo: errRepo}
	_, err := uc.Execute(plan.ID, workspace_plan.PlanMutationInput{Name: "Y", BasePriceBRLCents: 200, MaxCallChannels: 2})
	if err == nil {
		t.Fatal("expected error when ReplacePricingItems fails on update")
	}
}

func TestListPlanDefinitionsUseCase_RepoError(t *testing.T) {
	repo := &errTestPlanRepo{listErr: errors.New("db error")}
	uc := &listPlanDefinitionsUseCase{repo: repo}
	_, err := uc.Execute(false)
	if err == nil {
		t.Fatal("expected error when List fails")
	}
}

func TestGetPlanDefinitionUseCase_PlanNotFound(t *testing.T) {
	repo := newTestPlanRepo()
	uc := NewGetPlanDefinitionUseCase(repo)
	_, err := uc.Execute("nonexistent")
	if !errors.Is(err, workspace_plan.ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestCreatePlanDefinitionUseCase_ValidationError(t *testing.T) {
	repo := newTestPlanRepo()
	uc := NewCreatePlanDefinitionUseCase(repo)

	_, err := uc.Execute(workspace_plan.PlanMutationInput{
		Name:              "",
		BasePriceBRLCents: 100,
		MaxCallChannels:   1,
	})
	if !errors.Is(err, workspace_plan.ErrInvalidPlanName) {
		t.Fatalf("expected ErrInvalidPlanName, got %v", err)
	}
}

func TestUpdatePlanDefinitionUseCase_ValidationError(t *testing.T) {
	repo := newTestPlanRepo()
	createUC := NewCreatePlanDefinitionUseCase(repo)
	plan, _ := createUC.Execute(workspace_plan.PlanMutationInput{
		Name: "Test", BasePriceBRLCents: 100, MaxCallChannels: 1,
	})

	uc := NewUpdatePlanDefinitionUseCase(repo)
	_, err := uc.Execute(plan.ID, workspace_plan.PlanMutationInput{
		Name:              "",
		BasePriceBRLCents: 200,
		MaxCallChannels:   2,
	})
	if !errors.Is(err, workspace_plan.ErrInvalidPlanName) {
		t.Fatalf("expected ErrInvalidPlanName, got %v", err)
	}
}

func TestUpdatePlanDefinitionUseCase_ArchivedPlan(t *testing.T) {
	repo := newTestPlanRepo()
	fixedNow := time.Now()
	archived := fixedNow.Add(-time.Hour)
	repo.Create(&workspace_plan.PlanDefinition{
		ID: "plan-archived", Name: "Old", BasePriceBRLCents: 100, MaxCallChannels: 1,
		ArchivedAt: &archived,
	})

	uc := NewUpdatePlanDefinitionUseCase(repo)
	_, err := uc.Execute("plan-archived", workspace_plan.PlanMutationInput{
		Name: "New Name", BasePriceBRLCents: 200, MaxCallChannels: 2,
	})
	if !errors.Is(err, workspace_plan.ErrPlanArchived) {
		t.Fatalf("expected ErrPlanArchived, got %v", err)
	}
}

func TestSubscribeWorkspaceUseCase_CreateRepoError(t *testing.T) {
	plans := newTestPlanRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plans.Create(&workspace_plan.PlanDefinition{ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3})

	errSubs := &errTestSubscriptionRepo{
		testSubscriptionRepo: *newTestSubscriptionRepo(),
		createErr:            errors.New("db error"),
	}
	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: errSubs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "plan-1", workspace_plan.BillingCycleMonthly)
	if err == nil {
		t.Fatal("expected error when subscription repo.Create fails")
	}
}

func TestCreateSubscriptionInvoiceUseCase_PlanNotFound(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	inv := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "nonexistent", BillingType: "PIX"})
	if !errors.Is(err, workspace_plan.ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestCreateSubscriptionInvoiceUseCase_ExpiresOldBeforeCreating(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	inv := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3, IsGloballyVisible: true})

	subs.subscriptions["sub-old"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-old", WorkspaceID: "ws-1", PlanDefinitionID: "plan-1",
		PlanName: "Starter", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.AddDate(0, -3, 0),
		CurrentPeriodEnd:   fixedNow.AddDate(0, -2, 0),
	}

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	output, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if output.Invoice == nil {
		t.Fatal("expected invoice output")
	}

	if subs.subscriptions["sub-old"].Status != workspace_plan.SubscriptionStatusExpired {
		t.Fatalf("expected old sub expired, got %s", subs.subscriptions["sub-old"].Status)
	}
}

func TestRenewWorkspaceSubscriptionUseCase_PlanNotFound(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-gone",
		PlanName: "Gone", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusExpired,
		CurrentPeriodStart: fixedNow.AddDate(0, -2, 0),
		CurrentPeriodEnd:   fixedNow.AddDate(0, -1, 0),
	}

	uc := &renewWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1")
	if !errors.Is(err, workspace_plan.ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestCancelWorkspaceSubscriptionUseCase_PlanNotFound(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-gone",
		PlanName: "Gone", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-time.Hour),
		CurrentPeriodEnd:   fixedNow.Add(24 * time.Hour),
	}

	uc := &cancelWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1")
	if !errors.Is(err, workspace_plan.ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestGetWorkspaceSubscriptionUseCase_PlanNotFound(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-gone",
		PlanName: "Gone", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-time.Hour),
		CurrentPeriodEnd:   fixedNow.Add(24 * time.Hour),
	}

	uc := &getWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1")
	if !errors.Is(err, workspace_plan.ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

type errTestPlanRepo struct {
	testPlanRepo
	createErr         error
	updateErr         error
	replacePricingErr error
	listErr           error
}

func (r *errTestPlanRepo) Create(plan *workspace_plan.PlanDefinition) error {
	if r.createErr != nil {
		return r.createErr
	}
	return r.testPlanRepo.Create(plan)
}

func (r *errTestPlanRepo) Update(plan *workspace_plan.PlanDefinition) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.testPlanRepo.Update(plan)
}

func (r *errTestPlanRepo) ReplacePricingItems(planID string, items []workspace_plan.PlanPricingItem) error {
	if r.replacePricingErr != nil {
		return r.replacePricingErr
	}
	return r.testPlanRepo.ReplacePricingItems(planID, items)
}

func (r *errTestPlanRepo) List(includeArchived bool) ([]*workspace_plan.PlanDefinition, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.testPlanRepo.List(includeArchived)
}

type errTestSubscriptionRepo struct {
	testSubscriptionRepo
	createErr error
	updateErr error
}

func (r *errTestSubscriptionRepo) Create(sub *workspace_plan.WorkspaceSubscription) error {
	if r.createErr != nil {
		return r.createErr
	}
	return r.testSubscriptionRepo.Create(sub)
}

func (r *errTestSubscriptionRepo) Update(sub *workspace_plan.WorkspaceSubscription) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.testSubscriptionRepo.Update(sub)
}

type errPricingItemsRepo struct {
	testPlanRepo
	listPricingErr error
}

func (r *errPricingItemsRepo) ListPricingItems(planID string) ([]workspace_plan.PlanPricingItem, error) {
	if r.listPricingErr != nil {
		return nil, r.listPricingErr
	}
	return r.testPlanRepo.ListPricingItems(planID)
}

func TestSubscribeWorkspaceUseCase_UpdateExpiredSubError(t *testing.T) {
	plans := newTestPlanRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plans.Create(&workspace_plan.PlanDefinition{ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3})

	subs := &errTestSubscriptionRepo{
		testSubscriptionRepo: *newTestSubscriptionRepo(),
		updateErr:            errors.New("db error"),
	}

	subs.subscriptions["sub-old"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-old", WorkspaceID: "ws-1", PlanDefinitionID: "plan-1",
		PlanName: "Starter", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.AddDate(0, -3, 0),
		CurrentPeriodEnd:   fixedNow.AddDate(0, -2, 0),
	}

	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "plan-1", workspace_plan.BillingCycleMonthly)
	if err == nil {
		t.Fatal("expected error when Update of expired sub fails")
	}
}

func TestCreateSubscriptionInvoiceUseCase_UpdateExpiredSubError(t *testing.T) {
	plans := newTestPlanRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plans.Create(&workspace_plan.PlanDefinition{ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3})
	inv := &testInvoiceCreator{}

	subs := &errTestSubscriptionRepo{
		testSubscriptionRepo: *newTestSubscriptionRepo(),
		updateErr:            errors.New("db error"),
	}
	subs.subscriptions["sub-old"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-old", WorkspaceID: "ws-1", PlanDefinitionID: "plan-1",
		PlanName: "Starter", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.AddDate(0, -3, 0),
		CurrentPeriodEnd:   fixedNow.AddDate(0, -2, 0),
	}

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX"})
	if err == nil {
		t.Fatal("expected error when Update of expired sub fails on invoice creation")
	}
}

func TestRenewWorkspaceSubscriptionUseCase_UpdateError(t *testing.T) {
	plans := newTestPlanRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plans.Create(&workspace_plan.PlanDefinition{ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3})

	subs := &errTestSubscriptionRepo{
		testSubscriptionRepo: *newTestSubscriptionRepo(),
		updateErr:            errors.New("db error"),
	}
	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-1",
		PlanName: "Starter", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusExpired,
		CurrentPeriodStart: fixedNow.AddDate(0, -2, 0),
		CurrentPeriodEnd:   fixedNow.AddDate(0, -1, 0),
	}

	uc := &renewWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1")
	if err == nil {
		t.Fatal("expected error when Update fails on renew")
	}
}

func TestCancelWorkspaceSubscriptionUseCase_UpdateError(t *testing.T) {
	plans := newTestPlanRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plans.Create(&workspace_plan.PlanDefinition{ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3})

	subs := &errTestSubscriptionRepo{
		testSubscriptionRepo: *newTestSubscriptionRepo(),
		updateErr:            errors.New("db error"),
	}
	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-1",
		PlanName: "Starter", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-time.Hour),
		CurrentPeriodEnd:   fixedNow.Add(24 * time.Hour),
	}

	uc := &cancelWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1")
	if err == nil {
		t.Fatal("expected error when Update fails on cancel")
	}
}

func TestGetWorkspaceSubscriptionUseCase_UpdateError(t *testing.T) {
	plans := newTestPlanRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plans.Create(&workspace_plan.PlanDefinition{ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3})

	subs := &errTestSubscriptionRepo{
		testSubscriptionRepo: *newTestSubscriptionRepo(),
		updateErr:            errors.New("db error"),
	}

	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-1",
		PlanName: "Starter", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.AddDate(0, -2, 0),
		CurrentPeriodEnd:   fixedNow.AddDate(0, -1, 0),
	}

	uc := &getWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1")
	if err == nil {
		t.Fatal("expected error when Update fails on get with expired sub")
	}
}

func TestEnsureCurrentSubscription_LatestGetError(t *testing.T) {

	subs := &errLatestSubRepo{err: errors.New("db connection refused")}
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	uc := &ensureCurrentWorkspaceSubscriptionUseCase{subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1")
	if err == nil {
		t.Fatal("expected error propagated from GetLatest")
	}
}

func TestEnsureCurrentSubscription_UpdateExpiredError(t *testing.T) {
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	subs := &errTestSubscriptionRepo{
		testSubscriptionRepo: *newTestSubscriptionRepo(),
		updateErr:            errors.New("db error"),
	}

	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-1",
		PlanName: "Starter", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.AddDate(0, -3, 0),
		CurrentPeriodEnd:   fixedNow.AddDate(0, -2, 0),
	}

	uc := &ensureCurrentWorkspaceSubscriptionUseCase{subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1")
	if err == nil {
		t.Fatal("expected error when Update fails during expire")
	}
}

type errLatestSubRepo struct {
	err error
}

func (r *errLatestSubRepo) Create(*workspace_plan.WorkspaceSubscription) error { return nil }
func (r *errLatestSubRepo) Update(*workspace_plan.WorkspaceSubscription) error { return nil }
func (r *errLatestSubRepo) GetCurrentByWorkspaceID(string, time.Time) (*workspace_plan.WorkspaceSubscription, error) {
	return nil, workspace_plan.ErrSubscriptionNotCurrent
}
func (r *errLatestSubRepo) GetLatestByWorkspaceID(string) (*workspace_plan.WorkspaceSubscription, error) {
	return nil, r.err
}
func (r *errLatestSubRepo) GetCurrentByWorkspaceIDs([]string, time.Time) (map[string]*workspace_plan.WorkspaceSubscription, error) {
	return nil, nil
}
func (r *errLatestSubRepo) ExpireOverdue(time.Time, int) ([]string, error) { return nil, nil }
func (r *errLatestSubRepo) ListUpcomingExpirations(time.Time, time.Time, int) ([]*workspace_plan.WorkspaceSubscription, error) {
	return nil, nil
}
func (r *errLatestSubRepo) ListActiveBillingDue(time.Time, string, int) ([]*workspace_plan.WorkspaceSubscription, error) {
	return nil, nil
}

type errGetCurrentSubRepo struct {
	testSubscriptionRepo
	getCurrentErr error
	getLatestErr  error
}

func (r *errGetCurrentSubRepo) GetCurrentByWorkspaceID(string, time.Time) (*workspace_plan.WorkspaceSubscription, error) {
	if r.getCurrentErr != nil {
		return nil, r.getCurrentErr
	}
	return nil, workspace_plan.ErrSubscriptionNotCurrent
}

func (r *errGetCurrentSubRepo) GetLatestByWorkspaceID(string) (*workspace_plan.WorkspaceSubscription, error) {
	if r.getLatestErr != nil {
		return nil, r.getLatestErr
	}
	return nil, workspace_plan.ErrSubscriptionNotFound
}
func (r *errGetCurrentSubRepo) ListUpcomingExpirations(time.Time, time.Time, int) ([]*workspace_plan.WorkspaceSubscription, error) {
	return nil, nil
}
func (r *errGetCurrentSubRepo) ListActiveBillingDue(time.Time, string, int) ([]*workspace_plan.WorkspaceSubscription, error) {
	return nil, nil
}

func TestSubscribeWorkspaceUseCase_GetCurrentUnexpectedError(t *testing.T) {
	plans := newTestPlanRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plans.Create(&workspace_plan.PlanDefinition{ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3})

	subs := &errGetCurrentSubRepo{getCurrentErr: errors.New("db error")}
	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "plan-1", workspace_plan.BillingCycleMonthly)
	if err == nil || err.Error() != "db error" {
		t.Fatalf("expected 'db error', got %v", err)
	}
}

func TestSubscribeWorkspaceUseCase_GetLatestUnexpectedError(t *testing.T) {
	plans := newTestPlanRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plans.Create(&workspace_plan.PlanDefinition{ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3})

	subs := &errGetCurrentSubRepo{getLatestErr: errors.New("db error")}
	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "plan-1", workspace_plan.BillingCycleMonthly)
	if err == nil || err.Error() != "db error" {
		t.Fatalf("expected 'db error', got %v", err)
	}
}

type errExpireUpdateSubRepo struct {
	updateErr error
	latest    *workspace_plan.WorkspaceSubscription
}

func (r *errExpireUpdateSubRepo) Create(*workspace_plan.WorkspaceSubscription) error { return nil }
func (r *errExpireUpdateSubRepo) Update(*workspace_plan.WorkspaceSubscription) error {
	return r.updateErr
}
func (r *errExpireUpdateSubRepo) GetCurrentByWorkspaceID(string, time.Time) (*workspace_plan.WorkspaceSubscription, error) {
	return nil, workspace_plan.ErrSubscriptionNotCurrent
}
func (r *errExpireUpdateSubRepo) GetLatestByWorkspaceID(string) (*workspace_plan.WorkspaceSubscription, error) {
	return r.latest, nil
}
func (r *errExpireUpdateSubRepo) GetCurrentByWorkspaceIDs([]string, time.Time) (map[string]*workspace_plan.WorkspaceSubscription, error) {
	return nil, nil
}
func (r *errExpireUpdateSubRepo) ExpireOverdue(time.Time, int) ([]string, error) { return nil, nil }
func (r *errExpireUpdateSubRepo) ListUpcomingExpirations(time.Time, time.Time, int) ([]*workspace_plan.WorkspaceSubscription, error) {
	return nil, nil
}
func (r *errExpireUpdateSubRepo) ListActiveBillingDue(time.Time, string, int) ([]*workspace_plan.WorkspaceSubscription, error) {
	return nil, nil
}

func TestSubscribeWorkspaceUseCase_UpdateExpiredLatestError(t *testing.T) {
	plans := newTestPlanRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plans.Create(&workspace_plan.PlanDefinition{ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3})

	expired := &workspace_plan.WorkspaceSubscription{
		ID: "sub-old", WorkspaceID: "ws-1", PlanDefinitionID: "plan-1",
		PlanName: "Starter", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.AddDate(0, -2, 0),
		CurrentPeriodEnd:   fixedNow.AddDate(0, -1, 0),
	}
	subs := &errExpireUpdateSubRepo{updateErr: errors.New("update fail"), latest: expired}
	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "plan-1", workspace_plan.BillingCycleMonthly)
	if err == nil || err.Error() != "update fail" {
		t.Fatalf("expected 'update fail', got %v", err)
	}
}

func TestSubscribeWorkspaceUseCase_ValidateError(t *testing.T) {
	plans := newTestPlanRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{ID: " ", Name: "Bad", BasePriceBRLCents: 100, MaxCallChannels: 1})

	subs := newTestSubscriptionRepo()
	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", " ", workspace_plan.BillingCycleMonthly)
	if !errors.Is(err, workspace_plan.ErrInvalidSubscription) {
		t.Fatalf("expected ErrInvalidSubscription, got %v", err)
	}
}

func TestCreateSubscriptionInvoiceUseCase_GetCurrentUnexpectedError(t *testing.T) {
	plans := newTestPlanRepo()
	inv := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plans.Create(&workspace_plan.PlanDefinition{ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3})

	subs := &errGetCurrentSubRepo{getCurrentErr: errors.New("db error")}
	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX"})
	if err == nil || err.Error() != "db error" {
		t.Fatalf("expected 'db error', got %v", err)
	}
}

func TestCreateSubscriptionInvoiceUseCase_GetLatestUnexpectedError(t *testing.T) {
	plans := newTestPlanRepo()
	inv := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plans.Create(&workspace_plan.PlanDefinition{ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3})

	subs := &errGetCurrentSubRepo{getLatestErr: errors.New("db error")}
	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX"})
	if err == nil || err.Error() != "db error" {
		t.Fatalf("expected 'db error', got %v", err)
	}
}

func TestEnsureCurrentSubscription_GetCurrentUnexpectedError(t *testing.T) {
	subs := &errGetCurrentSubRepo{getCurrentErr: errors.New("db error")}
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	uc := &ensureCurrentWorkspaceSubscriptionUseCase{subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1")
	if err == nil || err.Error() != "db error" {
		t.Fatalf("expected 'db error', got %v", err)
	}
}

func TestRenewWorkspaceSubscription_ValidateError(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	plans.Create(&workspace_plan.PlanDefinition{ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3})

	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "", PlanDefinitionID: "plan-1",
		PlanName: "Starter", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusExpired,
		CurrentPeriodStart: fixedNow.AddDate(0, -2, 0),
		CurrentPeriodEnd:   fixedNow.AddDate(0, -1, 0),
	}

	uc := &renewWorkspaceSubscriptionUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("")
	if err == nil {
		t.Fatal("expected error for validation failure")
	}
}

func TestSetPlanVisibility_MakesRestricted(t *testing.T) {
	repo := newTestPlanRepo()
	repo.Create(&workspace_plan.PlanDefinition{
		ID: "plan-1", Name: "Starter", BasePriceBRLCents: 100, MaxCallChannels: 1,
		IsGloballyVisible: true,
	})
	repo.wsNames["ws-1"] = "Acme Corp"
	repo.wsNames["ws-2"] = "Beta Inc"

	uc := NewSetPlanVisibilityUseCase(repo)
	plan, err := uc.Execute("plan-1", workspace_plan.SetPlanVisibilityInput{
		IsGloballyVisible:   false,
		AllowedWorkspaceIDs: []string{"ws-1", "ws-2"},
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if plan.IsGloballyVisible {
		t.Fatal("expected plan to be restricted")
	}
	if len(plan.AllowedWorkspaceIDs) != 2 {
		t.Fatalf("expected 2 allowed IDs, got %d", len(plan.AllowedWorkspaceIDs))
	}
	if len(plan.AllowedWorkspaces) != 2 {
		t.Fatalf("expected 2 allowed workspaces, got %d", len(plan.AllowedWorkspaces))
	}
	if plan.AllowedWorkspaces[0].Name != "Acme Corp" {
		t.Fatalf("expected workspace name 'Acme Corp', got %q", plan.AllowedWorkspaces[0].Name)
	}
}

func TestSetPlanVisibility_MakesGlobal_ClearsList(t *testing.T) {
	repo := newTestPlanRepo()
	repo.Create(&workspace_plan.PlanDefinition{
		ID: "plan-1", Name: "Starter", BasePriceBRLCents: 100, MaxCallChannels: 1,
		IsGloballyVisible: false,
	})
	repo.visibility["plan-1"] = []string{"ws-1"}

	uc := NewSetPlanVisibilityUseCase(repo)
	plan, err := uc.Execute("plan-1", workspace_plan.SetPlanVisibilityInput{
		IsGloballyVisible:   true,
		AllowedWorkspaceIDs: nil,
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !plan.IsGloballyVisible {
		t.Fatal("expected plan to be globally visible")
	}
	if len(plan.AllowedWorkspaceIDs) != 0 {
		t.Fatalf("expected no allowed IDs, got %d", len(plan.AllowedWorkspaceIDs))
	}
	if len(plan.AllowedWorkspaces) != 0 {
		t.Fatalf("expected no allowed workspaces, got %d", len(plan.AllowedWorkspaces))
	}
	if _, ok := repo.visibility["plan-1"]; ok {
		t.Fatal("expected visibility entries to be cleared")
	}
}

func TestSetPlanVisibility_PlanNotFound(t *testing.T) {
	repo := newTestPlanRepo()
	uc := NewSetPlanVisibilityUseCase(repo)
	_, err := uc.Execute("nonexistent", workspace_plan.SetPlanVisibilityInput{
		IsGloballyVisible: true,
	})
	if !errors.Is(err, workspace_plan.ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestListVisiblePlans_ReturnsGlobalAndWhitelisted(t *testing.T) {
	repo := newTestPlanRepo()
	repo.Create(&workspace_plan.PlanDefinition{
		ID: "global-1", Name: "Public", BasePriceBRLCents: 100, MaxCallChannels: 1,
		IsGloballyVisible: true,
	})
	repo.Create(&workspace_plan.PlanDefinition{
		ID: "restricted-1", Name: "Private", BasePriceBRLCents: 200, MaxCallChannels: 2,
		IsGloballyVisible: false,
	})
	repo.visibility["restricted-1"] = []string{"ws-1"}

	uc := NewListVisiblePlansUseCase(repo, nil)
	plans, err := uc.Execute("ws-1")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(plans))
	}
}

func TestListVisiblePlans_ExcludesRestrictedForOtherWorkspace(t *testing.T) {
	repo := newTestPlanRepo()
	repo.Create(&workspace_plan.PlanDefinition{
		ID: "global-1", Name: "Public", BasePriceBRLCents: 100, MaxCallChannels: 1,
		IsGloballyVisible: true,
	})
	repo.Create(&workspace_plan.PlanDefinition{
		ID: "restricted-1", Name: "Private", BasePriceBRLCents: 200, MaxCallChannels: 2,
		IsGloballyVisible: false,
	})
	repo.visibility["restricted-1"] = []string{"ws-1"}

	uc := NewListVisiblePlansUseCase(repo, nil)
	plans, err := uc.Execute("ws-99")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan (only global), got %d", len(plans))
	}
	if plans[0].ID != "global-1" {
		t.Fatalf("expected global-1, got %s", plans[0].ID)
	}
}

func TestCreateSubscriptionInvoice_RejectsRestrictedPlan(t *testing.T) {
	plans := newTestPlanRepo()
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3,
		IsGloballyVisible: false,
	})
	plans.visibility["plan-1"] = []string{"ws-allowed"}

	inv := &testInvoiceCreator{}
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-denied", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX"})
	if !errors.Is(err, workspace_plan.ErrPlanNotVisible) {
		t.Fatalf("expected ErrPlanNotVisible, got %v", err)
	}
}

func TestCreateSubscriptionInvoice_AllowsWhitelistedWorkspace(t *testing.T) {
	plans := newTestPlanRepo()
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3,
		IsGloballyVisible: false,
	})
	plans.visibility["plan-1"] = []string{"ws-1"}

	inv := &testInvoiceCreator{}
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	result, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result == nil || result.Invoice == nil {
		t.Fatal("expected invoice to be created")
	}
}

func TestCreateSubscriptionInvoice_AllowsGlobalPlan(t *testing.T) {
	plans := newTestPlanRepo()
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3,
		IsGloballyVisible: true,
	})

	inv := &testInvoiceCreator{}
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	result, err := uc.Execute("ws-any", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result == nil || result.Invoice == nil {
		t.Fatal("expected invoice to be created")
	}
}

func TestListPlanDefinitions_PopulatesAllowedWorkspaces(t *testing.T) {
	repo := newTestPlanRepo()
	repo.Create(&workspace_plan.PlanDefinition{
		ID: "plan-1", Name: "Private", BasePriceBRLCents: 100, MaxCallChannels: 1,
		IsGloballyVisible: false,
	})
	repo.visibility["plan-1"] = []string{"ws-1"}
	repo.wsNames["ws-1"] = "Alpha"

	uc := NewListPlanDefinitionsUseCase(repo)
	plans, err := uc.Execute(false)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	if len(plans[0].AllowedWorkspaces) != 1 {
		t.Fatalf("expected 1 allowed workspace, got %d", len(plans[0].AllowedWorkspaces))
	}
	if plans[0].AllowedWorkspaces[0].Name != "Alpha" {
		t.Fatalf("expected 'Alpha', got %q", plans[0].AllowedWorkspaces[0].Name)
	}
}

func TestGetPlanDefinition_PopulatesAllowedWorkspaces(t *testing.T) {
	repo := newTestPlanRepo()
	repo.Create(&workspace_plan.PlanDefinition{
		ID: "plan-1", Name: "Private", BasePriceBRLCents: 100, MaxCallChannels: 1,
		IsGloballyVisible: false,
	})
	repo.visibility["plan-1"] = []string{"ws-1", "ws-2"}
	repo.wsNames["ws-1"] = "Alpha"
	repo.wsNames["ws-2"] = "Beta"

	uc := NewGetPlanDefinitionUseCase(repo)
	plan, err := uc.Execute("plan-1")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(plan.AllowedWorkspaces) != 2 {
		t.Fatalf("expected 2 allowed workspaces, got %d", len(plan.AllowedWorkspaces))
	}
}

func TestCreateSubscriptionInvoice_UpgradeAllowed(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	inv := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-starter", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3, IsGloballyVisible: true,
	})
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-pro", Name: "Pro", BasePriceBRLCents: 70000, MaxCallChannels: 6, IsGloballyVisible: true,
	})

	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-starter",
		PlanName: "Starter", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-24 * time.Hour), CurrentPeriodEnd: fixedNow.Add(24 * time.Hour),
	}

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	output, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-pro", BillingType: "PIX"})
	if err != nil {
		t.Fatalf("upgrade should be allowed, got error: %v", err)
	}
	if output.Invoice == nil {
		t.Fatal("expected invoice to be created")
	}
	if inv.lastInput.PlanDefinitionID != "plan-pro" {
		t.Fatalf("expected invoice for plan-pro, got %s", inv.lastInput.PlanDefinitionID)
	}
	if inv.lastInput.AmountBRL != 700 {
		t.Fatalf("expected Pro price 700, got %.2f", inv.lastInput.AmountBRL)
	}
}

func TestCreateSubscriptionInvoice_DowngradeBlocked(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	inv := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-pro", Name: "Pro", BasePriceBRLCents: 70000, MaxCallChannels: 6, IsGloballyVisible: true,
	})
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-starter", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3, IsGloballyVisible: true,
	})

	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-pro",
		PlanName: "Pro", MaxCallChannels: 6,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-24 * time.Hour), CurrentPeriodEnd: fixedNow.Add(24 * time.Hour),
	}

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-starter", BillingType: "PIX"})
	if !errors.Is(err, workspace_plan.ErrDowngradeNotAllowed) {
		t.Fatalf("expected ErrDowngradeNotAllowed, got %v", err)
	}
}

func TestCreateSubscriptionInvoice_SamePriceBlockedAsDowngrade(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	inv := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-a", Name: "Plan A", BasePriceBRLCents: 50000, MaxCallChannels: 4, IsGloballyVisible: true,
	})
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-b", Name: "Plan B", BasePriceBRLCents: 50000, MaxCallChannels: 5, IsGloballyVisible: true,
	})

	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-a",
		PlanName: "Plan A", MaxCallChannels: 4,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-24 * time.Hour), CurrentPeriodEnd: fixedNow.Add(24 * time.Hour),
	}

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-b", BillingType: "PIX"})
	if !errors.Is(err, workspace_plan.ErrDowngradeNotAllowed) {
		t.Fatalf("expected ErrDowngradeNotAllowed for same price, got %v", err)
	}
}

func TestCreateSubscriptionInvoice_UpgradeCurrentPlanNotFound(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	inv := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-pro", Name: "Pro", BasePriceBRLCents: 70000, MaxCallChannels: 6, IsGloballyVisible: true,
	})

	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-deleted",
		PlanName: "Deleted", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-24 * time.Hour), CurrentPeriodEnd: fixedNow.Add(24 * time.Hour),
	}

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-pro", BillingType: "PIX"})
	if !errors.Is(err, workspace_plan.ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound for deleted current plan, got %v", err)
	}
}

func TestCreateSubscriptionInvoice_UpgradeArchivedNewPlan(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	inv := &testInvoiceCreator{}
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-starter", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3, IsGloballyVisible: true,
	})
	archived := fixedNow.Add(-time.Hour)
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-pro", Name: "Pro", BasePriceBRLCents: 70000, MaxCallChannels: 6, IsGloballyVisible: true,
		ArchivedAt: &archived,
	})

	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-starter",
		PlanName: "Starter", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-24 * time.Hour), CurrentPeriodEnd: fixedNow.Add(24 * time.Hour),
	}

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-pro", BillingType: "PIX"})
	if !errors.Is(err, workspace_plan.ErrPlanArchived) {
		t.Fatalf("expected ErrPlanArchived, got %v", err)
	}
}

func TestSubscribeWorkspace_UpgradeReplacesActiveSubscription(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-starter", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3,
	})
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-pro", Name: "Pro", BasePriceBRLCents: 70000, MaxCallChannels: 6,
	})

	subs.subscriptions["sub-old"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-old", WorkspaceID: "ws-1", PlanDefinitionID: "plan-starter",
		PlanName: "Starter", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-24 * time.Hour), CurrentPeriodEnd: fixedNow.Add(24 * time.Hour),
	}

	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	details, err := uc.Execute("ws-1", "plan-pro", workspace_plan.BillingCycleMonthly)
	if err != nil {
		t.Fatalf("upgrade should be allowed, got error: %v", err)
	}
	if details.Subscription.PlanDefinitionID != "plan-pro" {
		t.Fatalf("expected new sub for plan-pro, got %s", details.Subscription.PlanDefinitionID)
	}
	if details.Subscription.PlanName != "Pro" {
		t.Fatalf("expected plan name Pro, got %s", details.Subscription.PlanName)
	}
	if details.Subscription.MaxCallChannels != 6 {
		t.Fatalf("expected 6 channels, got %d", details.Subscription.MaxCallChannels)
	}

	old := subs.subscriptions["sub-old"]
	if old.Status != workspace_plan.SubscriptionStatusExpired {
		t.Fatalf("expected old sub expired, got %s", old.Status)
	}
}

func TestSubscribeWorkspace_DowngradeBlockedByActiveSub(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-pro", Name: "Pro", BasePriceBRLCents: 70000, MaxCallChannels: 6,
	})
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-starter", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3,
	})

	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-pro",
		PlanName: "Pro", MaxCallChannels: 6,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-24 * time.Hour), CurrentPeriodEnd: fixedNow.Add(24 * time.Hour),
	}

	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "plan-starter", workspace_plan.BillingCycleMonthly)
	if !errors.Is(err, workspace_plan.ErrSubscriptionAlreadyExists) {
		t.Fatalf("expected ErrSubscriptionAlreadyExists for downgrade, got %v", err)
	}
}

func TestSubscribeWorkspace_UpgradeUpdateError(t *testing.T) {
	plans := newTestPlanRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-starter", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3,
	})
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-pro", Name: "Pro", BasePriceBRLCents: 70000, MaxCallChannels: 6,
	})

	subs := &errTestSubscriptionRepo{
		testSubscriptionRepo: *newTestSubscriptionRepo(),
		updateErr:            errors.New("db error"),
	}
	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-starter",
		PlanName: "Starter", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-24 * time.Hour), CurrentPeriodEnd: fixedNow.Add(24 * time.Hour),
	}

	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "plan-pro", workspace_plan.BillingCycleMonthly)
	if err == nil || err.Error() != "db error" {
		t.Fatalf("expected db error when expiring old sub, got %v", err)
	}
}

func TestSubscribeWorkspace_UpgradeCurrentPlanNotFound(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-pro", Name: "Pro", BasePriceBRLCents: 70000, MaxCallChannels: 6,
	})

	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-deleted",
		PlanName: "Deleted", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-24 * time.Hour), CurrentPeriodEnd: fixedNow.Add(24 * time.Hour),
	}

	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "plan-pro", workspace_plan.BillingCycleMonthly)
	if !errors.Is(err, workspace_plan.ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound for missing current plan, got %v", err)
	}
}

func TestSubscribeWorkspace_UpgradeNewPlanNotFound(t *testing.T) {
	plans := newTestPlanRepo()
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-starter", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3,
	})

	subs.subscriptions["sub-1"] = &workspace_plan.WorkspaceSubscription{
		ID: "sub-1", WorkspaceID: "ws-1", PlanDefinitionID: "plan-starter",
		PlanName: "Starter", MaxCallChannels: 3,
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow.Add(-24 * time.Hour), CurrentPeriodEnd: fixedNow.Add(24 * time.Hour),
	}

	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "plan-nonexistent", workspace_plan.BillingCycleMonthly)
	if !errors.Is(err, workspace_plan.ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound for missing new plan, got %v", err)
	}
}

func TestCreateSubscriptionInvoice_AnnualBillingCycleMultipliesBy12(t *testing.T) {
	plans := newTestPlanRepo()
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-1", Name: "Pro", BasePriceBRLCents: 10000, MaxCallChannels: 5, IsGloballyVisible: true,
	})

	inv := &testInvoiceCreator{}
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX", BillingCycle: "annual"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if inv.lastInput.AmountBRL != 1200 {
		t.Fatalf("expected annual amount 1200 BRL, got %.2f", inv.lastInput.AmountBRL)
	}
	if inv.lastInput.BillingCycle != "annual" {
		t.Fatalf("expected billingCycle 'annual', got %q", inv.lastInput.BillingCycle)
	}
}

func TestCreateSubscriptionInvoice_MonthlyBillingCycleUsesBasePrice(t *testing.T) {
	plans := newTestPlanRepo()
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3, IsGloballyVisible: true,
	})

	inv := &testInvoiceCreator{}
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX", BillingCycle: "monthly"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if inv.lastInput.AmountBRL != 199 {
		t.Fatalf("expected monthly amount 199 BRL, got %.2f", inv.lastInput.AmountBRL)
	}
	if inv.lastInput.BillingCycle != "monthly" {
		t.Fatalf("expected billingCycle 'monthly', got %q", inv.lastInput.BillingCycle)
	}
}

func TestCreateSubscriptionInvoice_EmptyBillingCycleDefaultsToMonthly(t *testing.T) {
	plans := newTestPlanRepo()
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3, IsGloballyVisible: true,
	})

	inv := &testInvoiceCreator{}
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if inv.lastInput.AmountBRL != 199 {
		t.Fatalf("expected default monthly amount 199 BRL, got %.2f", inv.lastInput.AmountBRL)
	}
	if inv.lastInput.BillingCycle != "monthly" {
		t.Fatalf("expected default billingCycle 'monthly', got %q", inv.lastInput.BillingCycle)
	}
}

func TestCreateSubscriptionInvoice_InvalidBillingCycleReturnsError(t *testing.T) {
	plans := newTestPlanRepo()
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3, IsGloballyVisible: true,
	})

	inv := &testInvoiceCreator{}
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	uc := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: inv, now: fixedClock(fixedNow)}
	_, err := uc.Execute("ws-1", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX", BillingCycle: "weekly"})
	if !errors.Is(err, workspace_plan.ErrInvalidSubscription) {
		t.Fatalf("expected ErrInvalidSubscription for invalid billing cycle, got %v", err)
	}
}

func TestCreateSubscriptionInvoice_AnnualAmountMatchesMonthlyTimesTwelve(t *testing.T) {

	plans := newTestPlanRepo()
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-1", Name: "Pro", BasePriceBRLCents: 50000, MaxCallChannels: 10, IsGloballyVisible: true,
	})

	invMonthly := &testInvoiceCreator{}
	invAnnual := &testInvoiceCreator{}
	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	ucM := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: invMonthly, now: fixedClock(fixedNow)}
	_, err := ucM.Execute("ws-m", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX", BillingCycle: "monthly"})
	if err != nil {
		t.Fatalf("monthly invoice error: %v", err)
	}

	ucA := &createSubscriptionInvoiceUseCase{plans: plans, subscriptions: subs, createInvoice: invAnnual, now: fixedClock(fixedNow)}
	_, err = ucA.Execute("ws-a", "user-1", workspace_plan.CreateSubscriptionInvoiceInput{PlanID: "plan-1", BillingType: "PIX", BillingCycle: "annual"})
	if err != nil {
		t.Fatalf("annual invoice error: %v", err)
	}

	if invAnnual.lastInput.AmountBRL != invMonthly.lastInput.AmountBRL*12 {
		t.Fatalf("annual (%.2f) must be 12× monthly (%.2f)", invAnnual.lastInput.AmountBRL, invMonthly.lastInput.AmountBRL)
	}
}

func TestSubscribeWorkspace_AnnualBillingCycleSets12MonthPeriod(t *testing.T) {
	plans := newTestPlanRepo()
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-1", Name: "Pro", BasePriceBRLCents: 50000, MaxCallChannels: 10,
	})

	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	details, err := uc.Execute("ws-1", "plan-1", workspace_plan.BillingCycleAnnual)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	expectedEnd := fixedNow.AddDate(0, 12, 0)
	if !details.Subscription.CurrentPeriodEnd.Equal(expectedEnd) {
		t.Fatalf("expected period end %v, got %v", expectedEnd, details.Subscription.CurrentPeriodEnd)
	}
	if details.Subscription.BillingCycle != workspace_plan.BillingCycleAnnual {
		t.Fatalf("expected billing cycle 'annual', got %q", details.Subscription.BillingCycle)
	}
}

func TestSubscribeWorkspace_MonthlyBillingCycleAlignsToAnchor(t *testing.T) {
	plans := newTestPlanRepo()
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3,
	})

	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	details, err := uc.Execute("ws-1", "plan-1", workspace_plan.BillingCycleMonthly)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// Monthly subscriptions co-term to the global billing anchor (the 23rd, in São Paulo time),
	// not a rolling +1 month from signup.
	expectedEnd := billing.PlanFirstAnchor(fixedNow.In(billing.LocationBRT()), billing.DefaultDueDay, billing.DefaultPlanFirstAnchorFloorDays)
	if !details.Subscription.CurrentPeriodEnd.Equal(expectedEnd) {
		t.Fatalf("expected anchored period end %v, got %v", expectedEnd, details.Subscription.CurrentPeriodEnd)
	}
	if details.Subscription.CurrentPeriodEnd.In(billing.LocationBRT()).Day() != billing.DefaultDueDay {
		t.Fatalf("period end should fall on the anchor day %d, got day %d",
			billing.DefaultDueDay, details.Subscription.CurrentPeriodEnd.In(billing.LocationBRT()).Day())
	}
	if details.Subscription.BillingCycle != workspace_plan.BillingCycleMonthly {
		t.Fatalf("expected billing cycle 'monthly', got %q", details.Subscription.BillingCycle)
	}
}

func TestSubscribeWorkspace_InvalidBillingCycleDefaultsToMonthly(t *testing.T) {
	plans := newTestPlanRepo()
	plans.Create(&workspace_plan.PlanDefinition{
		ID: "plan-1", Name: "Starter", BasePriceBRLCents: 19900, MaxCallChannels: 3,
	})

	subs := newTestSubscriptionRepo()
	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	uc := &subscribeWorkspaceUseCase{plans: plans, subscriptions: subs, now: fixedClock(fixedNow)}
	details, err := uc.Execute("ws-1", "plan-1", workspace_plan.BillingCycle("bogus"))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// An invalid cycle defaults to monthly, which anchors to the global billing day.
	expectedEnd := billing.PlanFirstAnchor(fixedNow.In(billing.LocationBRT()), billing.DefaultDueDay, billing.DefaultPlanFirstAnchorFloorDays)
	if !details.Subscription.CurrentPeriodEnd.Equal(expectedEnd) {
		t.Fatalf("expected anchored monthly period end %v, got %v", expectedEnd, details.Subscription.CurrentPeriodEnd)
	}
	if details.Subscription.BillingCycle != workspace_plan.BillingCycleMonthly {
		t.Fatalf("expected default billing cycle 'monthly', got %q", details.Subscription.BillingCycle)
	}
}
