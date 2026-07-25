package billing_usecase

import (
	"errors"
	"strings"
	"testing"
	"time"

	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

func previewFixture(subs *fakeSubs, plans *fakePlans, addons *fakeAddons, pricing *fakePricing) *previewMonthlyBillingUseCase {
	uc := NewPreviewMonthlyBillingUseCase(subs, plans, addons, pricing)
	uc.now = func() time.Time { return time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC) } // upcoming anchor = the 23rd
	return uc
}

// TestPreview_MatchesEmitChargeReadOnly checks the dry-run computes the same unified charge the emitter
// would (plan BRL + addons at FX, plan-only saldo) and writes nothing.
func TestPreview_MatchesEmitChargeReadOnly(t *testing.T) {
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{
		activeSub("ws-1", "plan-1"),
		activeSub("ws-2", "plan-1"),
	}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", BasePriceBRLCents: 50_000}}}
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{
		"ws-1": {channelAddon(25_000_000, 1)}, // one $25 channel; ws-2 has none
	}}

	report, err := previewFixture(subs, plans, addons, &fakePricing{rate: 6.0}).Execute()
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(report.Rows) != 2 || report.SkippedZero != 0 {
		t.Fatalf("expected 2 previewed rows, 0 skipped, got %d rows / %d skipped", len(report.Rows), report.SkippedZero)
	}

	byWS := map[string]WorkspaceBillingPreview{}
	for _, r := range report.Rows {
		byWS[r.WorkspaceID] = r
	}
	// ws-1: plan R$500 + $25 * 6 = R$150 -> R$650 total, R$500 creditable (plan only), one addon.
	if r := byWS["ws-1"]; r.TotalBRL != 650.00 || r.CreditableBRL != 500.00 || r.AddonCount != 1 {
		t.Fatalf("ws-1 preview wrong: %+v, want total 650 creditable 500 addons 1", r)
	}
	// ws-2: plan only, R$500 total == creditable, no addons.
	if r := byWS["ws-2"]; r.TotalBRL != 500.00 || r.CreditableBRL != 500.00 || r.AddonCount != 0 {
		t.Fatalf("ws-2 preview wrong: %+v, want total 500 creditable 500 addons 0", r)
	}
	if report.TotalBRL != 1150.00 {
		t.Fatalf("report total = %.2f, want 1150.00", report.TotalBRL)
	}
	// The anchor everyone is billed for is the 23rd.
	if d := byWS["ws-1"].BillingAnchor.Day(); d != 23 {
		t.Fatalf("billing anchor day = %d, want 23", d)
	}
	// Read-only: no subscription writes.
	if len(subs.updated) != 0 || len(addons.updated) != 0 {
		t.Fatalf("preview must not write anything: subs=%d addons=%d", len(subs.updated), len(addons.updated))
	}
}

// TestPreview_SkipsFreePlanWithNoAddons: a zero-price plan with no addons has nothing to bill and is
// counted as skipped, not previewed (mirrors emit's totalBRL <= 0 skip).
func TestPreview_SkipsFreePlanWithNoAddons(t *testing.T) {
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{activeSub("ws-free", "plan-free")}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-free": {ID: "plan-free", BasePriceBRLCents: 0}}}

	report, err := previewFixture(subs, plans, &fakeAddons{}, &fakePricing{rate: 6.0}).Execute()
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(report.Rows) != 0 || report.SkippedZero != 1 {
		t.Fatalf("a free plan with no addons must be skipped, got %d rows / %d skipped", len(report.Rows), report.SkippedZero)
	}
}

func TestPreview_PropagatesListError(t *testing.T) {
	subs := &fakeSubs{err: errors.New("db down")}
	if _, err := previewFixture(subs, &fakePlans{}, &fakeAddons{}, &fakePricing{rate: 6.0}).Execute(); err == nil {
		t.Fatal("a subscription read failure must surface as an error")
	}
}

func TestPreview_PropagatesPlanLookupError(t *testing.T) {
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{activeSub("ws-1", "missing-plan")}}
	if _, err := previewFixture(subs, &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{}}, &fakeAddons{}, &fakePricing{rate: 6.0}).Execute(); err == nil {
		t.Fatal("a missing plan must surface as an error, never a silently-wrong preview")
	}
}

// TestPreview_FallsBackOnPricingError: a pricing read failure must not block the preview; it falls back
// to the default FX just like the emitter does.
func TestPreview_FallsBackOnPricingError(t *testing.T) {
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{activeSub("ws-1", "plan-1")}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", BasePriceBRLCents: 50_000}}}
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{"ws-1": {channelAddon(25_000_000, 1)}}}

	report, err := previewFixture(subs, plans, addons, &fakePricing{err: errors.New("pricing down")}).Execute()
	if err != nil {
		t.Fatalf("a pricing error must fall back, not fail: %v", err)
	}
	// Default FX is 6.0, so the channel still adds R$150 -> R$650 total.
	if len(report.Rows) != 1 || report.Rows[0].TotalBRL != 650.00 {
		t.Fatalf("expected the fallback FX to still produce R$650, got %+v", report.Rows)
	}
}

func TestPreview_FormatRendersEachRow(t *testing.T) {
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{activeSub("ws-1", "plan-1")}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", BasePriceBRLCents: 50_000}}}
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{"ws-1": {channelAddon(25_000_000, 1)}}}

	report, err := previewFixture(subs, plans, addons, &fakePricing{rate: 6.0}).Execute()
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	out := report.Format()
	if !strings.Contains(out, "DRY-RUN") || !strings.Contains(out, "ws-1") || !strings.Contains(out, "650.00") {
		t.Fatalf("formatted report missing expected content:\n%s", out)
	}
}
