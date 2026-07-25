package billing_usecase

import (
	"errors"
	"sort"
	"testing"
	"time"

	billing "vozko/domain/billing"
	"vozko/domain/invoice"
	"vozko/domain/workspace"
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

// The fakes embed the real domain interfaces and override only the methods the emitter calls; any
// unused method stays nil and would panic if called, which keeps the fakes small while still binding
// to the actual repository contracts.

type fakeSubs struct {
	workspace_plan.SubscriptionRepository
	subs      []*workspace_plan.WorkspaceSubscription
	err       error
	latest    *workspace_plan.WorkspaceSubscription
	latestErr error
	updated   []*workspace_plan.WorkspaceSubscription
	updateErr error
}

// ListActiveBillingDue mimics the GORM query: active subscriptions only, ordered by id, keyset after
// afterID, capped at limit.
func (f *fakeSubs) ListActiveBillingDue(_ time.Time, afterID string, limit int) ([]*workspace_plan.WorkspaceSubscription, error) {
	if f.err != nil {
		return nil, f.err
	}
	var active []*workspace_plan.WorkspaceSubscription
	for _, s := range f.subs {
		if s.Status == workspace_plan.SubscriptionStatusActive && s.ID > afterID {
			active = append(active, s)
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].ID < active[j].ID })
	if len(active) > limit {
		active = active[:limit]
	}
	return active, nil
}

func (f *fakeSubs) GetLatestByWorkspaceID(string) (*workspace_plan.WorkspaceSubscription, error) {
	return f.latest, f.latestErr
}

func (f *fakeSubs) Update(s *workspace_plan.WorkspaceSubscription) error {
	f.updated = append(f.updated, s)
	return f.updateErr
}

type fakePlans struct {
	plans map[string]*workspace_plan.PlanDefinition
}

func (f *fakePlans) GetByID(id string) (*workspace_plan.PlanDefinition, error) {
	if p, ok := f.plans[id]; ok {
		return p, nil
	}
	return nil, errors.New("plan not found")
}

type fakeAddons struct {
	workspace_addon.AddonSubscriptionRepository
	byWS             map[string][]*workspace_addon.AddonSubscription
	err              error
	updated          []*workspace_addon.AddonSubscription
	updateErr        error
	reactivatedSince time.Time // captures the expiredSince argument the confirm passes
}

func (f *fakeAddons) ListActiveByWorkspace(ws string) ([]*workspace_addon.AddonSubscription, error) {
	return f.byWS[ws], f.err
}

func (f *fakeAddons) ListReactivatableByWorkspace(ws string, expiredSince time.Time) ([]*workspace_addon.AddonSubscription, error) {
	f.reactivatedSince = expiredSince
	return f.byWS[ws], f.err
}

func (f *fakeAddons) Update(a *workspace_addon.AddonSubscription) error {
	f.updated = append(f.updated, a)
	return f.updateErr
}

type fakeWorkspaces struct {
	workspace.Repository
	byWS map[string]*workspace.Workspace
}

func (f *fakeWorkspaces) GetWorkspaceByID(id string) (*workspace.Workspace, error) {
	return f.byWS[id], nil
}

type fakePricing struct {
	workspace_pricing.Repository
	rate float64 // BRL per USD; 0 means "no item configured" so the helper falls back
	err  error
}

func (f *fakePricing) ListDefaultPricingItems() ([]workspace_pricing.PricingItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.rate == 0 {
		return nil, nil
	}
	return []workspace_pricing.PricingItem{{
		Category:    workspace_pricing.CategoryExchangeRate,
		Service:     "usd_to_brl",
		PriceMicros: int64(f.rate * 1_000_000),
	}}, nil
}

type fakeCreateInvoice struct {
	calls []invoice.CreateInvoiceInput
	err   error
}

func (f *fakeCreateInvoice) Execute(in invoice.CreateInvoiceInput) (*invoice.CreateInvoiceOutput, error) {
	f.calls = append(f.calls, in)
	if f.err != nil {
		return nil, f.err
	}
	return &invoice.CreateInvoiceOutput{Invoice: &invoice.Invoice{ID: "inv-" + in.IdempotencyKey, WorkspaceID: in.WorkspaceID}}, nil
}

// TestEmit_BuildsLineItemsAndAnchorDueDate checks the unified invoice carries a customer-facing,
// price-only breakdown (plan line credited to saldo + channel line as pass-through) and is due on the
// 23rd anchor. The InvoiceLineItem type structurally cannot carry cost.
func TestEmit_BuildsLineItemsAndAnchorDueDate(t *testing.T) {
	addon := channelAddon(25_000_000, 1)
	addon.AddonKey = "whatsapp_channel"
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{activeSub("ws-1", "plan-1")}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", Name: "Pro", BasePriceBRLCents: 50_000}}}
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{"ws-1": {addon}}}
	wss := &fakeWorkspaces{byWS: map[string]*workspace.Workspace{"ws-1": owner("ws-1", "user-1")}}
	inv := &fakeCreateInvoice{}

	if n, err := emitFixture(subs, plans, addons, wss, inv).Execute(); err != nil || n != 1 {
		t.Fatalf("emit: n=%d err=%v", n, err)
	}
	in := inv.calls[0]
	// Plan R$500 + one $25 channel * FX 6 = R$150 -> R$650 total, R$500 creditable (plan only).
	if in.AmountBRL != 650 || in.CreditableBRL != 500 {
		t.Fatalf("amounts: total=%.2f creditable=%.2f, want 650/500", in.AmountBRL, in.CreditableBRL)
	}
	if in.DueDate == nil || in.DueDate.In(billing.LocationBRT()).Day() != 23 {
		t.Fatalf("invoice must be due on the 23rd anchor, got %v", in.DueDate)
	}
	if len(in.LineItems) != 2 {
		t.Fatalf("want 2 line items (plan + channel), got %d: %+v", len(in.LineItems), in.LineItems)
	}
	if p := in.LineItems[0]; p.Kind != invoice.LineItemPlan || p.AmountBRL != 500 || !p.Creditable {
		t.Fatalf("plan line must be R$500, credited to saldo: %+v", p)
	}
	if c := in.LineItems[1]; c.Kind != invoice.LineItemChannel || c.AmountBRL != 150 || c.Creditable || c.Label != "whatsapp_channel" || c.Quantity != 1 {
		t.Fatalf("channel line must be R$150, pass-through, labelled, qty 1: %+v", c)
	}
}

func activeSub(ws, plan string) *workspace_plan.WorkspaceSubscription {
	return &workspace_plan.WorkspaceSubscription{
		ID:               "sub-" + ws,
		WorkspaceID:      ws,
		PlanDefinitionID: plan,
		Status:           workspace_plan.SubscriptionStatusActive,
		BillingCycle:     workspace_plan.BillingCycleMonthly,
		CurrentPeriodEnd: time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC),
	}
}

func channelAddon(usdMicros int64, qty int) *workspace_addon.AddonSubscription {
	return &workspace_addon.AddonSubscription{
		EntitlementKind: workspace_addon.EntitlementWhatsAppBusinessPhones,
		Status:          workspace_plan.SubscriptionStatusActive,
		Quantity:        qty,
		UnitPriceMicros: usdMicros,
	}
}

func owner(ws, userID string) *workspace.Workspace {
	return &workspace.Workspace{ID: ws, OwnerID: userID}
}

// emitFixture wires the usecase with a fixed clock at 2026-03-18 (so the upcoming anchor is the 23rd)
// and an FX of 6.0 BRL/USD.
func emitFixture(subs *fakeSubs, plans *fakePlans, addons *fakeAddons, wss *fakeWorkspaces, inv *fakeCreateInvoice) *emitMonthlyInvoicesUseCase {
	return emitWith(subs, plans, addons, wss, &fakePricing{rate: 6.0}, inv)
}

func emitWith(subs *fakeSubs, plans *fakePlans, addons *fakeAddons, wss *fakeWorkspaces, pricing *fakePricing, inv *fakeCreateInvoice) *emitMonthlyInvoicesUseCase {
	uc := NewEmitMonthlyInvoicesUseCase(subs, plans, addons, wss, pricing, inv)
	uc.now = func() time.Time { return time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC) }
	return uc
}

func TestEmit_HappyPath_OneUnifiedInvoice(t *testing.T) {
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{activeSub("ws-1", "plan-1")}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", BasePriceBRLCents: 109_900}}}
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{
		"ws-1": {channelAddon(25_000_000, 1), channelAddon(25_000_000, 1)}, // two $25 channels
	}}
	wss := &fakeWorkspaces{byWS: map[string]*workspace.Workspace{"ws-1": owner("ws-1", "user-1")}}
	inv := &fakeCreateInvoice{}

	n, err := emitFixture(subs, plans, addons, wss, inv).Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 1 || len(inv.calls) != 1 {
		t.Fatalf("expected one invoice, emitted=%d calls=%d", n, len(inv.calls))
	}
	got := inv.calls[0]
	if got.Purpose != invoice.PurposeMonthlyBilling {
		t.Errorf("purpose = %q, want MONTHLY_BILLING", got.Purpose)
	}
	if got.AmountBRL != 1399.00 { // 1099 + 2*(25*6)
		t.Errorf("AmountBRL = %.2f, want 1399.00", got.AmountBRL)
	}
	if got.CreditableBRL != 1099.00 {
		t.Errorf("CreditableBRL = %.2f, want 1099.00 (plan only)", got.CreditableBRL)
	}
	if got.UserID != "user-1" || got.PlanDefinitionID != "plan-1" || got.BillingType != "PIX" {
		t.Errorf("unexpected invoice fields: %+v", got)
	}
	if got.IdempotencyKey != "monthly:ws-1:2026-03-23" {
		t.Errorf("IdempotencyKey = %q, want monthly:ws-1:2026-03-23", got.IdempotencyKey)
	}
}

// TestEmit_AnnualPlanChargedFullYear is the annual-cycle guard: an annual subscription must be billed
// the full-year plan price (base * 12, less any annual discount), because payment confirmation extends
// it a full 12 months. Charging a single month here would grant a year of access for a month's money.
func TestEmit_AnnualPlanChargedFullYear(t *testing.T) {
	annual := activeSub("ws-1", "plan-1")
	annual.BillingCycle = workspace_plan.BillingCycleAnnual
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{annual}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", Name: "Pro", BasePriceBRLCents: 50_000}}}
	wss := &fakeWorkspaces{byWS: map[string]*workspace.Workspace{"ws-1": owner("ws-1", "user-1")}}
	inv := &fakeCreateInvoice{}

	if n, err := emitFixture(subs, plans, &fakeAddons{}, wss, inv).Execute(); err != nil || n != 1 {
		t.Fatalf("emit: n=%d err=%v", n, err)
	}
	got := inv.calls[0]
	// Plan R$500/mo on an annual cycle -> R$6000 for the year (no discount configured today).
	want := float64(workspace_plan.BillingCycleAnnual.TotalPriceBRLCents(50_000)) / 100.0
	if want != 6000.00 {
		t.Fatalf("precondition: annual price = %.2f, want 6000.00", want)
	}
	if got.AmountBRL != want || got.CreditableBRL != want {
		t.Fatalf("annual plan must be charged the full year: total=%.2f creditable=%.2f, want %.2f", got.AmountBRL, got.CreditableBRL, want)
	}
	if got.BillingCycle != string(workspace_plan.BillingCycleAnnual) {
		t.Fatalf("invoice billing cycle = %q, want annual", got.BillingCycle)
	}
	if len(got.LineItems) != 1 || got.LineItems[0].AmountBRL != want || !got.LineItems[0].Creditable {
		t.Fatalf("annual plan line must show the full-year price credited to saldo: %+v", got.LineItems)
	}
}

func TestEmit_CancelledNotBilled(t *testing.T) {
	cancelled := activeSub("ws-1", "plan-1")
	cancelled.Status = workspace_plan.SubscriptionStatusCancelled
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{cancelled}}
	inv := &fakeCreateInvoice{}

	n, err := emitFixture(subs, &fakePlans{}, &fakeAddons{}, &fakeWorkspaces{}, inv).Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 0 || len(inv.calls) != 0 {
		t.Fatalf("a cancelled subscription must not be billed, emitted=%d calls=%d", n, len(inv.calls))
	}
}

func TestEmit_KeysetPaginationCoversEveryWorkspace(t *testing.T) {
	// Five workspaces but a page size of two: keyset pagination must emit all five without looping.
	var list []*workspace_plan.WorkspaceSubscription
	wsMap := map[string]*workspace.Workspace{}
	for _, ws := range []string{"ws-1", "ws-2", "ws-3", "ws-4", "ws-5"} {
		list = append(list, activeSub(ws, "plan-1"))
		wsMap[ws] = owner(ws, "user-"+ws)
	}
	subs := &fakeSubs{subs: list}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", BasePriceBRLCents: 50_000}}}
	wss := &fakeWorkspaces{byWS: wsMap}
	inv := &fakeCreateInvoice{}

	uc := emitFixture(subs, plans, &fakeAddons{}, wss, inv)
	uc.batchSize = 2
	n, err := uc.Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 5 || len(inv.calls) != 5 {
		t.Fatalf("keyset pagination should emit all five workspaces, emitted=%d calls=%d", n, len(inv.calls))
	}
}

func TestEmit_PerWorkspaceFailureIsolated(t *testing.T) {
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{
		activeSub("ws-1", "plan-1"),
		activeSub("ws-2", "missing-plan"), // GetByID fails for this one
	}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", BasePriceBRLCents: 50_000}}}
	wss := &fakeWorkspaces{byWS: map[string]*workspace.Workspace{"ws-1": owner("ws-1", "user-1")}}
	inv := &fakeCreateInvoice{}

	n, err := emitFixture(subs, plans, &fakeAddons{}, wss, inv).Execute()
	if err != nil {
		t.Fatalf("one bad workspace must not fail the run: %v", err)
	}
	if n != 1 || len(inv.calls) != 1 || inv.calls[0].WorkspaceID != "ws-1" {
		t.Fatalf("expected only ws-1 emitted, emitted=%d calls=%+v", n, inv.calls)
	}
}

func TestEmit_ZeroTotalSkipped(t *testing.T) {
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{activeSub("ws-1", "free")}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"free": {ID: "free", BasePriceBRLCents: 0}}}
	wss := &fakeWorkspaces{byWS: map[string]*workspace.Workspace{"ws-1": owner("ws-1", "user-1")}}
	inv := &fakeCreateInvoice{}

	n, err := emitFixture(subs, plans, &fakeAddons{}, wss, inv).Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 0 || len(inv.calls) != 0 {
		t.Fatalf("a free plan with no addons must not produce an invoice, emitted=%d calls=%d", n, len(inv.calls))
	}
}

func TestEmit_MissingOwnerSkipped(t *testing.T) {
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{activeSub("ws-1", "plan-1")}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", BasePriceBRLCents: 50_000}}}
	wss := &fakeWorkspaces{byWS: map[string]*workspace.Workspace{}} // no owner row
	inv := &fakeCreateInvoice{}

	n, err := emitFixture(subs, plans, &fakeAddons{}, wss, inv).Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 0 || len(inv.calls) != 0 {
		t.Fatalf("a workspace with no owner must be skipped, emitted=%d calls=%d", n, len(inv.calls))
	}
}

func TestEmit_SubsRepoErrorPropagates(t *testing.T) {
	subs := &fakeSubs{err: errors.New("db down")}
	inv := &fakeCreateInvoice{}
	if _, err := emitFixture(subs, &fakePlans{}, &fakeAddons{}, &fakeWorkspaces{}, inv).Execute(); err == nil {
		t.Fatal("expected the subscription query error to propagate")
	}
}

func TestEmit_FXErrorUsesFallbackRate(t *testing.T) {
	// A transient pricing-read failure must not block billing; it falls back to the default rate
	// (mirroring create_invoice), so the workspace is still billed.
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{activeSub("ws-1", "plan-1")}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", BasePriceBRLCents: 50_000}}}
	wss := &fakeWorkspaces{byWS: map[string]*workspace.Workspace{"ws-1": owner("ws-1", "user-1")}}
	inv := &fakeCreateInvoice{}
	uc := emitWith(subs, plans, &fakeAddons{}, wss, &fakePricing{err: errors.New("pricing down")}, inv)
	n, err := uc.Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 1 || len(inv.calls) != 1 {
		t.Fatalf("FX failure should fall back and still bill, emitted=%d calls=%d", n, len(inv.calls))
	}
	if inv.calls[0].AmountBRL != 500.00 { // 50_000 cents, no addons
		t.Fatalf("AmountBRL = %.2f, want 500.00", inv.calls[0].AmountBRL)
	}
}

func TestEmit_AddonListErrorSkipsWorkspace(t *testing.T) {
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{activeSub("ws-1", "plan-1")}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", BasePriceBRLCents: 50_000}}}
	addons := &fakeAddons{err: errors.New("addons db down")}
	inv := &fakeCreateInvoice{}
	n, err := emitFixture(subs, plans, addons, &fakeWorkspaces{}, inv).Execute()
	if err != nil {
		t.Fatalf("a per-workspace addon error must not fail the run: %v", err)
	}
	if n != 0 || len(inv.calls) != 0 {
		t.Fatalf("addon list failure must skip the workspace, emitted=%d calls=%d", n, len(inv.calls))
	}
}

func TestEmit_CreateInvoiceErrorSkipsWorkspace(t *testing.T) {
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{activeSub("ws-1", "plan-1")}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", BasePriceBRLCents: 50_000}}}
	wss := &fakeWorkspaces{byWS: map[string]*workspace.Workspace{"ws-1": owner("ws-1", "user-1")}}
	inv := &fakeCreateInvoice{err: errors.New("asaas down")}
	n, err := emitFixture(subs, plans, &fakeAddons{}, wss, inv).Execute()
	if err != nil {
		t.Fatalf("a create-invoice error must not fail the run: %v", err)
	}
	if n != 0 {
		t.Fatalf("a failed invoice must not be counted as emitted, emitted=%d", n)
	}
}

func TestBRTNow_IsBillingZoneAndTracksWallClock(t *testing.T) {
	got := brtNow()
	if got.Location() != billing.LocationBRT() {
		t.Fatalf("brtNow should report in the billing zone, got %s", got.Location())
	}
	if d := time.Since(got); d < -time.Minute || d > time.Minute {
		t.Fatalf("brtNow should track the wall clock, delta=%s", d)
	}
}
