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
	uc.now = func() time.Time { return time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC) }
	return uc
}

func TestPreview_MatchesEmitChargeReadOnly(t *testing.T) {
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{
		activeSub("ws-1", "plan-1"),
		activeSub("ws-2", "plan-1"),
	}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", BasePriceBRLCents: 50_000}}}
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{
		"ws-1": {channelAddon(25_000_000, 1)},
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
	if r := byWS["ws-1"]; r.TotalBRL != 650.00 || r.CreditableBRL != 500.00 || r.AddonCount != 1 {
		t.Fatalf("ws-1 preview wrong: %+v, want total 650 creditable 500 addons 1", r)
	}
	if r := byWS["ws-2"]; r.TotalBRL != 500.00 || r.CreditableBRL != 500.00 || r.AddonCount != 0 {
		t.Fatalf("ws-2 preview wrong: %+v, want total 500 creditable 500 addons 0", r)
	}
	if report.TotalBRL != 1150.00 {
		t.Fatalf("report total = %.2f, want 1150.00", report.TotalBRL)
	}
	if d := byWS["ws-1"].BillingAnchor.Day(); d != 23 {
		t.Fatalf("billing anchor day = %d, want 23", d)
	}
	if len(subs.updated) != 0 || len(addons.updated) != 0 {
		t.Fatalf("preview must not write anything: subs=%d addons=%d", len(subs.updated), len(addons.updated))
	}
}

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

func TestPreview_FallsBackOnPricingError(t *testing.T) {
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{activeSub("ws-1", "plan-1")}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", BasePriceBRLCents: 50_000}}}
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{"ws-1": {channelAddon(25_000_000, 1)}}}

	report, err := previewFixture(subs, plans, addons, &fakePricing{err: errors.New("pricing down")}).Execute()
	if err != nil {
		t.Fatalf("a pricing error must fall back, not fail: %v", err)
	}
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
