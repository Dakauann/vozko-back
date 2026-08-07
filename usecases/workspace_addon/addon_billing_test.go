package workspace_addon_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/balance"
	billing "vozko/domain/billing"
	"vozko/domain/shared"
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

var fixedNow = time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)

func testClock() time.Time { return fixedNow }

type fakeDefRepo struct {
	defs map[string]*workspace_addon.AddonDefinition
}

func newFakeDefRepo(defs ...*workspace_addon.AddonDefinition) *fakeDefRepo {
	m := map[string]*workspace_addon.AddonDefinition{}
	for _, d := range defs {
		m[d.ID] = d
	}
	return &fakeDefRepo{defs: m}
}

func (r *fakeDefRepo) Create(d *workspace_addon.AddonDefinition) error { r.defs[d.ID] = d; return nil }
func (r *fakeDefRepo) Update(d *workspace_addon.AddonDefinition) error { r.defs[d.ID] = d; return nil }
func (r *fakeDefRepo) Archive(id string, _ time.Time) error            { return nil }
func (r *fakeDefRepo) GetByID(id string) (*workspace_addon.AddonDefinition, error) {
	if d, ok := r.defs[id]; ok {
		return d, nil
	}
	return nil, workspace_addon.ErrAddonNotFound
}
func (r *fakeDefRepo) GetByKey(string) (*workspace_addon.AddonDefinition, error) {
	return nil, workspace_addon.ErrAddonNotFound
}
func (r *fakeDefRepo) List(bool) ([]*workspace_addon.AddonDefinition, error) { return nil, nil }
func (r *fakeDefRepo) ListActiveVisible(string) ([]*workspace_addon.AddonDefinition, error) {
	return nil, nil
}

type fakeSubRepo struct {
	subs      map[string]*workspace_addon.AddonSubscription
	createErr error
}

func newFakeSubRepo(subs ...*workspace_addon.AddonSubscription) *fakeSubRepo {
	m := map[string]*workspace_addon.AddonSubscription{}
	for _, s := range subs {
		m[s.ID] = s
	}
	return &fakeSubRepo{subs: m}
}

func (r *fakeSubRepo) Create(s *workspace_addon.AddonSubscription) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.subs[s.ID] = s
	return nil
}
func (r *fakeSubRepo) Update(s *workspace_addon.AddonSubscription) error {
	r.subs[s.ID] = s
	return nil
}
func (r *fakeSubRepo) GetByID(id string) (*workspace_addon.AddonSubscription, error) {
	if s, ok := r.subs[id]; ok {
		return s, nil
	}
	return nil, workspace_addon.ErrAddonSubscriptionNotFound
}
func (r *fakeSubRepo) GetActiveByWorkspaceAndDefinition(ws, defID string) (*workspace_addon.AddonSubscription, error) {
	for _, s := range r.subs {
		if s.WorkspaceID == ws && s.AddonDefinitionID == defID && s.Status == workspace_plan.SubscriptionStatusActive {
			return s, nil
		}
	}
	return nil, workspace_addon.ErrAddonSubscriptionNotFound
}
func (r *fakeSubRepo) GetActiveByBoundResource(string, string) (*workspace_addon.AddonSubscription, error) {
	return nil, workspace_addon.ErrAddonSubscriptionNotFound
}
func (r *fakeSubRepo) ListActiveByWorkspaceAndKind(ws string, kind workspace_addon.EntitlementKind) ([]*workspace_addon.AddonSubscription, error) {
	var out []*workspace_addon.AddonSubscription
	for _, s := range r.subs {
		if s.WorkspaceID == ws && s.EntitlementKind == kind && s.Status == workspace_plan.SubscriptionStatusActive {
			out = append(out, s)
		}
	}
	return out, nil
}
func (r *fakeSubRepo) ListActiveByWorkspace(ws string) ([]*workspace_addon.AddonSubscription, error) {
	var out []*workspace_addon.AddonSubscription
	for _, s := range r.subs {
		if s.WorkspaceID == ws && s.Status == workspace_plan.SubscriptionStatusActive {
			out = append(out, s)
		}
	}
	return out, nil
}
func (r *fakeSubRepo) ListReactivatableByWorkspace(ws string, expiredSince time.Time) ([]*workspace_addon.AddonSubscription, error) {
	var out []*workspace_addon.AddonSubscription
	for _, s := range r.subs {
		if s.WorkspaceID != ws {
			continue
		}
		active := s.Status == workspace_plan.SubscriptionStatusActive
		swept := s.Status == workspace_plan.SubscriptionStatusExpired && s.CancelledAt == nil && !s.CurrentPeriodEnd.Before(expiredSince)
		if active || swept {
			out = append(out, s)
		}
	}
	return out, nil
}
func (r *fakeSubRepo) ListDueForRenewal(at time.Time, _ int) ([]*workspace_addon.AddonSubscription, error) {
	var out []*workspace_addon.AddonSubscription
	for _, s := range r.subs {
		if s.Status == workspace_plan.SubscriptionStatusActive && !s.CurrentPeriodEnd.After(at) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (r *fakeSubRepo) SumActiveGrantedUnitsByWorkspaceIDs(_ []string, _ workspace_addon.EntitlementKind) (map[string]int, error) {
	return map[string]int{}, nil
}

func (r *fakeSubRepo) ListUpcomingRenewals(_, _ time.Time, _ int) ([]*workspace_addon.AddonSubscription, error) {
	return nil, nil
}

type fakeBalanceRepo struct {
	amount  int64
	byRef   map[string]bool
	debits  []balance.DebitBalanceInput
	credits []balance.CreditBalanceInput
}

func newFakeBalanceRepo(amount int64) *fakeBalanceRepo {
	return &fakeBalanceRepo{amount: amount, byRef: map[string]bool{}}
}

func (r *fakeBalanceRepo) Create(*balance.Balance) error { return nil }
func (r *fakeBalanceRepo) GetByWorkspaceID(ws string) (*balance.Balance, error) {
	return &balance.Balance{WorkspaceID: ws, Amount: r.amount, Currency: "USD"}, nil
}
func (r *fakeBalanceRepo) EnsureBalanceExists(ws, cur string) (*balance.Balance, error) {
	return &balance.Balance{WorkspaceID: ws, Amount: r.amount, Currency: cur}, nil
}
func (r *fakeBalanceRepo) CreditBalance(p balance.CreditBalanceInput) (*balance.Transaction, error) {
	r.amount += p.Amount
	if p.ReferenceID != nil {
		r.byRef[*p.ReferenceID] = true
	}
	r.credits = append(r.credits, p)
	return &balance.Transaction{}, nil
}
func (r *fakeBalanceRepo) DebitBalance(p balance.DebitBalanceInput) (*balance.Transaction, error) {
	if !p.AllowNegative && r.amount < p.Amount {
		return nil, balance.ErrInsufficientBalance
	}
	r.amount -= p.Amount
	if p.ReferenceID != nil {
		r.byRef[*p.ReferenceID] = true
	}
	r.debits = append(r.debits, p)
	return &balance.Transaction{}, nil
}
func (r *fakeBalanceRepo) HasSufficientBalance(_ string, amount int64) (bool, error) {
	return r.amount >= amount, nil
}
func (r *fakeBalanceRepo) GetFullBalanceSummary(string) (*balance.FullBalanceSummary, error) {
	return nil, nil
}
func (r *fakeBalanceRepo) GetTransaction(string) (*balance.Transaction, error) { return nil, nil }
func (r *fakeBalanceRepo) ListTransactions(balance.ListTransactionsInput) (*shared.PaginatedResult[*balance.Transaction], error) {
	return nil, nil
}
func (r *fakeBalanceRepo) ExistsTransactionByReferenceID(ref string) (bool, error) {
	return r.byRef[ref], nil
}
func (r *fakeBalanceRepo) AggregateDailyCosts(time.Time) ([]balance.DailyCostRow, error) {
	return nil, nil
}

type fakeCurrentSub struct {
	sub *workspace_plan.WorkspaceSubscription
}

func (f *fakeCurrentSub) GetCurrentByWorkspaceID(string, time.Time) (*workspace_plan.WorkspaceSubscription, error) {
	if f.sub == nil {
		return nil, workspace_plan.ErrSubscriptionNotCurrent
	}
	return f.sub, nil
}

type fakePlanReader struct {
	plan *workspace_plan.PlanDefinition
}

func (f *fakePlanReader) GetByID(string) (*workspace_plan.PlanDefinition, error) {
	if f.plan == nil {
		return nil, workspace_plan.ErrPlanNotFound
	}
	return f.plan, nil
}

type fakeChangeHandler struct {
	calls     []string
	increased []string
}

func (h *fakeChangeHandler) OnEntitlementReduced(ws string, kind workspace_addon.EntitlementKind) error {
	h.calls = append(h.calls, ws+":"+string(kind))
	return nil
}

func (h *fakeChangeHandler) OnEntitlementIncreased(ws string, kind workspace_addon.EntitlementKind) error {
	h.increased = append(h.increased, ws+":"+string(kind))
	return nil
}

func activeDef(id string, kind workspace_addon.EntitlementKind, monthlyMicros int64) *workspace_addon.AddonDefinition {
	return &workspace_addon.AddonDefinition{
		ID: id, Key: id, Name: id, EntitlementKind: kind, UnitsPerQuantity: 1,
		MonthlyPriceMicros: monthlyMicros, AnnualPriceMicros: monthlyMicros * 10,
		IsActive: true, IsGloballyVisible: true,
	}
}

func TestEntitlementResolver_BasePlusAddons(t *testing.T) {
	subs := newFakeSubRepo(&workspace_addon.AddonSubscription{
		ID: "s1", WorkspaceID: "ws", AddonDefinitionID: "d1",
		EntitlementKind: workspace_addon.EntitlementCallChannels,
		Quantity:        2, UnitsPerQuantity: 1, Status: workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: fixedNow, CurrentPeriodEnd: fixedNow.AddDate(0, 1, 0),
	})
	r := &entitlementResolver{
		subscriptions: &fakeCurrentSub{sub: &workspace_plan.WorkspaceSubscription{PlanDefinitionID: "p1", Status: workspace_plan.SubscriptionStatusActive}},
		plans:         &fakePlanReader{plan: &workspace_plan.PlanDefinition{ID: "p1", MaxCallChannels: 3}},
		addons:        subs,
		now:           testClock,
	}
	got, err := r.Resolve("ws", workspace_addon.EntitlementCallChannels)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != 5 {
		t.Fatalf("expected 5 (3 base + 2 addon), got %d", got)
	}
}

func TestEntitlementResolver_ErrorsWithoutActiveSubscription(t *testing.T) {
	r := &entitlementResolver{
		subscriptions: &fakeCurrentSub{sub: nil},
		plans:         &fakePlanReader{},
		addons:        newFakeSubRepo(),
		now:           testClock,
	}
	if _, err := r.Resolve("ws", workspace_addon.EntitlementCallChannels); err == nil {
		t.Fatal("expected error when no active subscription")
	}
}

func TestPurchaseAddon_DebitsWalletAndActivates(t *testing.T) {
	defs := newFakeDefRepo(activeDef("d1", workspace_addon.EntitlementCallChannels, 5_000_000))
	subs := newFakeSubRepo()
	bal := newFakeBalanceRepo(10_000_000)
	uc := &purchaseAddonUseCase{defs: defs, subs: subs, balanceRepo: bal, now: testClock}

	sub, err := uc.Execute("ws", workspace_addon.PurchaseAddonInput{AddonDefinitionID: "d1", Quantity: 1, BillingCycle: workspace_plan.BillingCycleMonthly})
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}
	if sub.Status != workspace_plan.SubscriptionStatusActive || sub.Quantity != 1 {
		t.Fatalf("unexpected sub: %+v", sub)
	}
	// fixedNow is June 28 (after the 18th emit day), so the channel's first invoice is the NEXT anchor,
	// July 23. It prorates [Jun 28, Jul 23] = 25 days over the 30-day Jun23->Jul23 cycle: $5 * 25/30 =
	// $4.166667. 10M - 4_166_667 = 5_833_333. Never more than a month, and it co-terms to the 23rd.
	if bal.amount != 5_833_333 {
		t.Fatalf("expected prorated debit leaving 5_833_333, got %d", bal.amount)
	}
	end := sub.CurrentPeriodEnd.In(billing.LocationBRT())
	if end.Month() != time.July || end.Day() != 23 {
		t.Fatalf("expected period end = the July 23 anchor, got %s", end.Format("2006-01-02"))
	}
	if len(bal.debits) != 1 || bal.debits[0].ServiceType != balance.ServiceAddon {
		t.Fatalf("expected one addon debit, got %+v", bal.debits)
	}
}

func TestPurchaseAddon_InsufficientBalance(t *testing.T) {
	defs := newFakeDefRepo(activeDef("d1", workspace_addon.EntitlementCallChannels, 5_000_000))
	subs := newFakeSubRepo()
	bal := newFakeBalanceRepo(1_000_000)
	uc := &purchaseAddonUseCase{defs: defs, subs: subs, balanceRepo: bal, now: testClock}

	_, err := uc.Execute("ws", workspace_addon.PurchaseAddonInput{AddonDefinitionID: "d1", Quantity: 1, BillingCycle: workspace_plan.BillingCycleMonthly})
	if err != balance.ErrInsufficientBalance {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
	if len(subs.subs) != 0 {
		t.Fatalf("expected no subscription created on insufficient balance, got %d", len(subs.subs))
	}
	if bal.amount != 1_000_000 {
		t.Fatalf("expected wallet untouched, got %d", bal.amount)
	}
}

func TestPurchaseAddon_ProratesBeforeAnchorWithinSameMonth(t *testing.T) {
	defs := newFakeDefRepo(activeDef("d1", workspace_addon.EntitlementCallChannels, 5_000_000))
	subs := newFakeSubRepo()
	bal := newFakeBalanceRepo(10_000_000)
	// June 10 is before the 18th emit day, so the channel is on THIS month's invoice (June 23). It
	// prorates [Jun 10, Jun 23] = 13 days over the 31-day May23->Jun23 cycle: $5 * 13/31 = $2.096774.
	// 10M - 2_096_774 = 7_903_226, and it co-terms to the June 23 anchor.
	uc := &purchaseAddonUseCase{defs: defs, subs: subs, balanceRepo: bal, now: func() time.Time {
		return time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	}}

	sub, err := uc.Execute("ws", workspace_addon.PurchaseAddonInput{AddonDefinitionID: "d1", Quantity: 1, BillingCycle: workspace_plan.BillingCycleMonthly})
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}
	if bal.amount != 7_903_226 {
		t.Fatalf("expected 7_903_226 after a prorate-to-anchor charge, got %d", bal.amount)
	}
	end := sub.CurrentPeriodEnd.In(billing.LocationBRT())
	if end.Month() != time.June || end.Day() != 23 {
		t.Fatalf("expected period end = the June 23 anchor, got %s", end.Format("2006-01-02"))
	}
}

// TestPurchaseAddon_RefundsOnPersistFailure is the money-critical rollback: if the wallet is debited
// but the subscription cannot be saved, the debit must be refunded so the customer is never charged for
// a channel they did not get.
func TestPurchaseAddon_RefundsOnPersistFailure(t *testing.T) {
	defs := newFakeDefRepo(activeDef("d1", workspace_addon.EntitlementCallChannels, 5_000_000))
	subs := newFakeSubRepo()
	subs.createErr = errors.New("db write failed")
	bal := newFakeBalanceRepo(10_000_000)
	uc := &purchaseAddonUseCase{defs: defs, subs: subs, balanceRepo: bal, now: testClock}

	_, err := uc.Execute("ws", workspace_addon.PurchaseAddonInput{AddonDefinitionID: "d1", Quantity: 1, BillingCycle: workspace_plan.BillingCycleMonthly})
	if err == nil {
		t.Fatal("a persist failure must surface as an error")
	}
	if bal.amount != 10_000_000 {
		t.Fatalf("the debit must be refunded on persist failure, wallet = %d, want 10_000_000 restored", bal.amount)
	}
	if len(bal.credits) != 1 || !bal.credits[0].IsRefund {
		t.Fatalf("expected exactly one refund credit, got %+v", bal.credits)
	}
	if len(subs.subs) != 0 {
		t.Fatalf("no subscription should remain after a failed create, got %d", len(subs.subs))
	}
}

// TestPurchaseAddon_TopUpExistingAddonChargesFullPeriod covers the isNew=false path: adding quantity to
// an existing active addon increments its quantity and charges the full period (no activation stub).
func TestPurchaseAddon_TopUpExistingAddonChargesFullPeriod(t *testing.T) {
	existing := &workspace_addon.AddonSubscription{
		ID: "existing-1", WorkspaceID: "ws", AddonDefinitionID: "d1", AddonKey: "d1",
		EntitlementKind: workspace_addon.EntitlementCallChannels, Quantity: 1, UnitsPerQuantity: 1,
		BillingCycle: workspace_plan.BillingCycleMonthly, Status: workspace_plan.SubscriptionStatusActive,
		UnitPriceMicros:    5_000_000,
		CurrentPeriodStart: fixedNow.AddDate(0, 0, -5), CurrentPeriodEnd: fixedNow.AddDate(0, 0, 10),
	}
	defs := newFakeDefRepo(activeDef("d1", workspace_addon.EntitlementCallChannels, 5_000_000))
	subs := newFakeSubRepo(existing)
	bal := newFakeBalanceRepo(10_000_000)
	uc := &purchaseAddonUseCase{defs: defs, subs: subs, balanceRepo: bal, now: testClock}

	sub, err := uc.Execute("ws", workspace_addon.PurchaseAddonInput{AddonDefinitionID: "d1", Quantity: 1, BillingCycle: workspace_plan.BillingCycleMonthly})
	if err != nil {
		t.Fatalf("top-up: %v", err)
	}
	if sub.ID != "existing-1" || sub.Quantity != 2 {
		t.Fatalf("expected the existing addon quantity bumped to 2, got id=%s qty=%d", sub.ID, sub.Quantity)
	}
	// A top-up charges the full unit price (no activation proration), so 10M - 5M = 5M.
	if bal.amount != 5_000_000 {
		t.Fatalf("expected a full-period debit leaving 5_000_000, got %d", bal.amount)
	}
}

// TestPurchaseAddon_AnnualUsesAnnualPriceAndYearPeriod covers the annual branch (annual price, +12
// months, no activation stub).
func TestPurchaseAddon_AnnualUsesAnnualPriceAndYearPeriod(t *testing.T) {
	defs := newFakeDefRepo(activeDef("d1", workspace_addon.EntitlementCallChannels, 5_000_000)) // annual = 50M
	subs := newFakeSubRepo()
	bal := newFakeBalanceRepo(100_000_000)
	uc := &purchaseAddonUseCase{defs: defs, subs: subs, balanceRepo: bal, now: testClock}

	sub, err := uc.Execute("ws", workspace_addon.PurchaseAddonInput{AddonDefinitionID: "d1", Quantity: 1, BillingCycle: workspace_plan.BillingCycleAnnual})
	if err != nil {
		t.Fatalf("annual purchase: %v", err)
	}
	if bal.amount != 50_000_000 {
		t.Fatalf("expected the annual price (50M) debited, leaving 50M, got %d", bal.amount)
	}
	if !sub.CurrentPeriodEnd.After(fixedNow.AddDate(0, 11, 0)) {
		t.Fatalf("annual period must extend ~12 months, got %s", sub.CurrentPeriodEnd.Format("2006-01-02"))
	}
}

func TestPurchaseAddon_RejectsInactiveDefinition(t *testing.T) {
	inactive := activeDef("d1", workspace_addon.EntitlementCallChannels, 5_000_000)
	inactive.IsActive = false
	defs := newFakeDefRepo(inactive)
	uc := &purchaseAddonUseCase{defs: defs, subs: newFakeSubRepo(), balanceRepo: newFakeBalanceRepo(10_000_000), now: testClock}

	if _, err := uc.Execute("ws", workspace_addon.PurchaseAddonInput{AddonDefinitionID: "d1", Quantity: 1}); err != workspace_addon.ErrAddonInactive {
		t.Fatalf("expected ErrAddonInactive, got %v", err)
	}
}

func TestPurchaseAddon_RejectsInvalidInput(t *testing.T) {
	uc := &purchaseAddonUseCase{defs: newFakeDefRepo(), subs: newFakeSubRepo(), balanceRepo: newFakeBalanceRepo(10_000_000), now: testClock}
	if _, err := uc.Execute("", workspace_addon.PurchaseAddonInput{AddonDefinitionID: "d1"}); err != workspace_addon.ErrInvalidAddonSubscription {
		t.Fatalf("empty workspace must be rejected, got %v", err)
	}
	if _, err := uc.Execute("ws", workspace_addon.PurchaseAddonInput{AddonDefinitionID: ""}); err != workspace_addon.ErrInvalidAddonSubscription {
		t.Fatalf("empty definition must be rejected, got %v", err)
	}
}

// TestPurchaseAddon_FreeAddonActivatesWithoutDebit covers the amount<=0 path: a zero-priced addon
// activates without touching the wallet.
func TestPurchaseAddon_FreeAddonActivatesWithoutDebit(t *testing.T) {
	defs := newFakeDefRepo(activeDef("free", workspace_addon.EntitlementCallChannels, 0))
	subs := newFakeSubRepo()
	bal := newFakeBalanceRepo(0)
	uc := &purchaseAddonUseCase{defs: defs, subs: subs, balanceRepo: bal, now: testClock}

	sub, err := uc.Execute("ws", workspace_addon.PurchaseAddonInput{AddonDefinitionID: "free", Quantity: 1, BillingCycle: workspace_plan.BillingCycleMonthly})
	if err != nil {
		t.Fatalf("free addon purchase: %v", err)
	}
	if sub.Status != workspace_plan.SubscriptionStatusActive {
		t.Fatal("a free addon should still activate")
	}
	if len(bal.debits) != 0 {
		t.Fatalf("a free addon must not debit the wallet, got %+v", bal.debits)
	}
}

// TestPreviewAddonPurchase_MatchesCharge: the preview must quote exactly what the purchase charges (both
// go through billing.ActivationPeriod). fixedNow is June 28 (after the emit day), so the first invoice is
// July 23: prorate [Jun 28, Jul 23] = 25 days over 30 = $4.166667 = the same debit as
// TestPurchaseAddon_DebitsWalletAndActivates.
func TestPreviewAddonPurchase_MatchesCharge(t *testing.T) {
	defs := newFakeDefRepo(activeDef("d1", workspace_addon.EntitlementCallChannels, 5_000_000))
	uc := &previewAddonPurchaseUseCase{defs: defs, subs: newFakeSubRepo(), now: testClock}

	preview, err := uc.Execute("ws", workspace_addon.PurchaseAddonInput{AddonDefinitionID: "d1", Quantity: 1, BillingCycle: workspace_plan.BillingCycleMonthly})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.ChargeNowMicros != 4_166_667 {
		t.Fatalf("chargeNow = %d, want 4_166_667 (must equal the actual purchase debit)", preview.ChargeNowMicros)
	}
	if preview.RecurringMicros != 5_000_000 {
		t.Fatalf("recurring = %d, want 5_000_000 (the steady-state monthly)", preview.RecurringMicros)
	}
	if preview.ChargeNowMicros > preview.RecurringMicros {
		t.Fatalf("you-pay-now (%d) must never exceed one month (%d)", preview.ChargeNowMicros, preview.RecurringMicros)
	}
	if !preview.Prorated || preview.ProratedDays != 25 {
		t.Fatalf("proration wrong: %+v (want prorated=true, 25 days to the July 23 anchor)", preview)
	}
}

func TestPreviewAddonPurchase_BeforeEmitDayBillsThisMonthAnchor(t *testing.T) {
	defs := newFakeDefRepo(activeDef("d1", workspace_addon.EntitlementCallChannels, 5_000_000))
	uc := &previewAddonPurchaseUseCase{defs: defs, subs: newFakeSubRepo(), now: func() time.Time {
		return time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC) // before the 18th emit day
	}}

	preview, err := uc.Execute("ws", workspace_addon.PurchaseAddonInput{AddonDefinitionID: "d1", Quantity: 1, BillingCycle: workspace_plan.BillingCycleMonthly})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	// First invoice is THIS month's June 23: prorate [Jun 10, Jun 23] = 13 days over 31 = $2.096774.
	if preview.ChargeNowMicros != 2_096_774 || preview.ProratedDays != 13 {
		t.Fatalf("before-emit-day preview wrong: %+v (want 2_096_774, 13 days)", preview)
	}
}

func TestPreviewAddonPurchase_AnnualNotProrated(t *testing.T) {
	defs := newFakeDefRepo(activeDef("d1", workspace_addon.EntitlementCallChannels, 5_000_000)) // annual = 50M
	uc := &previewAddonPurchaseUseCase{defs: defs, subs: newFakeSubRepo(), now: testClock}

	preview, err := uc.Execute("ws", workspace_addon.PurchaseAddonInput{AddonDefinitionID: "d1", Quantity: 1, BillingCycle: workspace_plan.BillingCycleAnnual})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.ChargeNowMicros != 50_000_000 || preview.Prorated {
		t.Fatalf("annual must charge the full price with no proration, got %+v", preview)
	}
}

func TestNewPurchaseAddonUseCase_Constructor(t *testing.T) {
	if uc := NewPurchaseAddonUseCase(newFakeDefRepo(), newFakeSubRepo(), newFakeBalanceRepo(0), &fakeChangeHandler{}); uc == nil {
		t.Fatal("expected a non-nil use case")
	}
}
