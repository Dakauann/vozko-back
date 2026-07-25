package billing_usecase

import (
	"errors"
	"testing"
	"time"

	billing "vozko/domain/billing"
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

// confirmFixture pins the clock at payment time on the 23rd, so extension rolls to the next anchor.
func confirmFixture(subs *fakeSubs, addons *fakeAddons) *confirmMonthlyBillingUseCase {
	uc := NewConfirmMonthlyBillingUseCase(subs, addons)
	uc.now = func() time.Time { return time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC) }
	return uc
}

func planSub(ws string, end time.Time) *workspace_plan.WorkspaceSubscription {
	return &workspace_plan.WorkspaceSubscription{
		ID: "sub-" + ws, WorkspaceID: ws, PlanDefinitionID: "plan-1",
		Status: workspace_plan.SubscriptionStatusActive, BillingCycle: workspace_plan.BillingCycleMonthly,
		CurrentPeriodEnd: end,
	}
}

func addonSub(end time.Time) *workspace_addon.AddonSubscription {
	return &workspace_addon.AddonSubscription{
		ID: "addon-1", Status: workspace_plan.SubscriptionStatusActive,
		BillingCycle: workspace_plan.BillingCycleMonthly, Quantity: 1, UnitPriceMicros: 25_000_000,
		CurrentPeriodEnd: end,
	}
}

func isApril23(t *testing.T, label string, got time.Time) {
	t.Helper()
	in := got.In(billing.LocationBRT())
	if in.Month() != time.April || in.Day() != 23 {
		t.Fatalf("%s should roll to the next anchor (Apr 23), got %s", label, in.Format("2006-01-02"))
	}
}

func TestConfirm_ExtendsPlanAndAddons(t *testing.T) {
	anchor := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC) // the period just paid
	subs := &fakeSubs{latest: planSub("ws-1", anchor)}
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{
		"ws-1": {addonSub(anchor)},
	}}

	if err := confirmFixture(subs, addons).Execute("ws-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(subs.updated) != 1 {
		t.Fatalf("expected the plan subscription to be updated once, got %d", len(subs.updated))
	}
	isApril23(t, "plan period end", subs.updated[0].CurrentPeriodEnd)
	if subs.updated[0].Status != workspace_plan.SubscriptionStatusActive {
		t.Errorf("plan should stay active after extension")
	}
	if len(addons.updated) != 1 {
		t.Fatalf("expected one addon updated, got %d", len(addons.updated))
	}
	isApril23(t, "addon period end", addons.updated[0].CurrentPeriodEnd)
}

func TestConfirm_NoPlanSubscriptionStillExtendsAddons(t *testing.T) {
	anchor := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)
	subs := &fakeSubs{latest: nil} // no plan subscription found
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{"ws-1": {addonSub(anchor)}}}

	if err := confirmFixture(subs, addons).Execute("ws-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(subs.updated) != 0 {
		t.Fatalf("no plan subscription means no plan update, got %d", len(subs.updated))
	}
	if len(addons.updated) != 1 {
		t.Fatalf("addons should still be extended, got %d", len(addons.updated))
	}
}

func TestConfirm_PlanLookupErrorIsSkippedNotFatal(t *testing.T) {
	subs := &fakeSubs{latestErr: errors.New("db blip")}
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{"ws-1": {addonSub(time.Time{})}}}

	if err := confirmFixture(subs, addons).Execute("ws-1"); err != nil {
		t.Fatalf("a plan-lookup error must not fail confirmation: %v", err)
	}
	if len(addons.updated) != 1 {
		t.Fatalf("addons should still be extended, got %d", len(addons.updated))
	}
}

func TestConfirm_PlanUpdateErrorPropagates(t *testing.T) {
	subs := &fakeSubs{latest: planSub("ws-1", time.Now()), updateErr: errors.New("write failed")}
	addons := &fakeAddons{}
	if err := confirmFixture(subs, addons).Execute("ws-1"); err == nil {
		t.Fatal("expected the plan update error to propagate")
	}
}

func TestConfirm_AddonUpdateErrorPropagates(t *testing.T) {
	subs := &fakeSubs{latest: nil}
	addons := &fakeAddons{
		byWS:      map[string][]*workspace_addon.AddonSubscription{"ws-1": {addonSub(time.Now())}},
		updateErr: errors.New("write failed"),
	}
	if err := confirmFixture(subs, addons).Execute("ws-1"); err == nil {
		t.Fatal("expected the addon update error to propagate")
	}
}

func TestConfirm_AddonListErrorPropagates(t *testing.T) {
	subs := &fakeSubs{latest: nil}
	addons := &fakeAddons{err: errors.New("list failed")}
	if err := confirmFixture(subs, addons).Execute("ws-1"); err == nil {
		t.Fatal("expected the addon list error to propagate")
	}
}

// TestConfirm_ConvergesAnyScatteredDateToAnchor is the proof of the self-alignment claim: a customer
// on ANY prior renewal date, once they pay a single unified invoice, lands on the global 23rd anchor.
// No migration is required for alignment; the payment-confirmation Extend snaps every subscription to
// the next anchor. This is Maria's case (last paid Mar 7) generalized across past, this-month, and
// far-future period ends.
func TestConfirm_ConvergesAnyScatteredDateToAnchor(t *testing.T) {
	// Payment lands on Mar 23; each sub starts on a different scattered renewal date.
	scattered := []struct {
		name string
		end  time.Time
	}{
		{"paid through the 7th (past)", time.Date(2026, 3, 7, 0, 0, 0, 0, time.UTC)},
		{"paid through the 15th (past)", time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)},
		{"paid through the 28th (later this month)", time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC)},
		{"paid through Apr 10 (next month)", time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)},
		{"paid far ahead to Jun 15", time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)},
		{"already on the anchor", time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range scattered {
		t.Run(tc.name, func(t *testing.T) {
			subs := &fakeSubs{latest: planSub("ws-1", tc.end)}
			addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{
				"ws-1": {addonSub(tc.end)},
			}}
			if err := confirmFixture(subs, addons).Execute("ws-1"); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			// The whole claim: whatever the start date, the new period end is the 23rd (in the billing tz).
			planDay := subs.updated[0].CurrentPeriodEnd.In(billing.LocationBRT()).Day()
			if planDay != billing.DefaultDueDay {
				t.Fatalf("plan starting %s did not converge to the anchor: landed on day %d, want %d",
					tc.end.Format("2006-01-02"), planDay, billing.DefaultDueDay)
			}
			addonDay := addons.updated[0].CurrentPeriodEnd.In(billing.LocationBRT()).Day()
			if addonDay != billing.DefaultDueDay {
				t.Fatalf("addon starting %s did not converge to the anchor: landed on day %d, want %d",
					tc.end.Format("2006-01-02"), addonDay, billing.DefaultDueDay)
			}
			// And the new period is strictly in the future of the payment, never backdated.
			if !subs.updated[0].CurrentPeriodEnd.After(time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)) {
				t.Fatalf("period end %s must be after the payment date", subs.updated[0].CurrentPeriodEnd)
			}
		})
	}
}

func expiredPlanSub(ws string, end time.Time) *workspace_plan.WorkspaceSubscription {
	s := planSub(ws, end)
	s.Status = workspace_plan.SubscriptionStatusExpired
	return s
}

func expiredAddonSub(end time.Time) *workspace_addon.AddonSubscription {
	a := addonSub(end)
	a.Status = workspace_plan.SubscriptionStatusExpired
	return a
}

// TestConfirm_LatePaymentRevivesSweptSubsAndReactivatesChannels is the step-9 core: a workspace the
// cancel sweep already expired (plan and addon expired, channel suspended at the vendor) pays late. The
// confirm must revive both subscriptions to active, roll them to the next anchor, and reactivate the
// suspended channels through the entitlement handler.
func TestConfirm_LatePaymentRevivesSweptSubsAndReactivatesChannels(t *testing.T) {
	anchor := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC) // the unpaid cycle the sweep expired
	subs := &fakeSubs{latest: expiredPlanSub("ws-1", anchor)}
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{"ws-1": {expiredAddonSub(anchor)}}}
	handler := &fakeEntitlementHandler{}

	uc := confirmFixture(subs, addons).WithReactivation(handler)
	if err := uc.Execute("ws-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if subs.updated[0].Status != workspace_plan.SubscriptionStatusActive {
		t.Errorf("a swept plan must be revived to active on payment")
	}
	isApril23(t, "revived plan period end", subs.updated[0].CurrentPeriodEnd)
	if addons.updated[0].Status != workspace_plan.SubscriptionStatusActive {
		t.Errorf("a swept addon must be revived to active on payment")
	}
	isApril23(t, "revived addon period end", addons.updated[0].CurrentPeriodEnd)

	if len(handler.increased) != 1 || handler.increased[0] != "ws-1" {
		t.Fatalf("reviving a swept subscription must reactivate channels, got increased=%v", handler.increased)
	}
	// The revival window is the prior anchor (now - 1 month), so an earlier-cycle lapse is excluded.
	wantSince := time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC)
	if !addons.reactivatedSince.Equal(wantSince) {
		t.Errorf("expiredSince = %s, want the prior anchor %s", addons.reactivatedSince, wantSince)
	}
}

// TestConfirm_OnTimePaymentDoesNotReactivate: a normal on-time payment (subs still active) extends the
// periods but must NOT call the reactivation handler, since nothing was suspended.
func TestConfirm_OnTimePaymentDoesNotReactivate(t *testing.T) {
	anchor := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)
	subs := &fakeSubs{latest: planSub("ws-1", anchor)}
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{"ws-1": {addonSub(anchor)}}}
	handler := &fakeEntitlementHandler{}

	if err := confirmFixture(subs, addons).WithReactivation(handler).Execute("ws-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(handler.increased) != 0 {
		t.Fatalf("an on-time payment must not reactivate (nothing was suspended), got %v", handler.increased)
	}
}

// TestConfirm_RevivalWithoutHandlerDoesNotPanic: reactivation is best-effort. With no handler wired, a
// revival still extends the subscriptions and must not panic.
func TestConfirm_RevivalWithoutHandlerDoesNotPanic(t *testing.T) {
	anchor := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)
	subs := &fakeSubs{latest: nil}
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{"ws-1": {expiredAddonSub(anchor)}}}

	if err := confirmFixture(subs, addons).Execute("ws-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if addons.updated[0].Status != workspace_plan.SubscriptionStatusActive {
		t.Errorf("the addon should still be revived even without a reactivation handler")
	}
}

// TestConfirm_ReactivationFailureIsNotFatal: if the vendor reactivation fails, the confirmed payment
// must still succeed (the subscriptions are already extended; the reconcile job retries the channel).
func TestConfirm_ReactivationFailureIsNotFatal(t *testing.T) {
	anchor := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)
	subs := &fakeSubs{latest: nil}
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{"ws-1": {expiredAddonSub(anchor)}}}
	handler := &fakeEntitlementHandler{failWS: map[string]error{"ws-1": errors.New("vendor down")}}

	if err := confirmFixture(subs, addons).WithReactivation(handler).Execute("ws-1"); err != nil {
		t.Fatalf("a failed reactivation must not fail the payment: %v", err)
	}
	if len(handler.increased) != 1 {
		t.Fatalf("the reactivation must have been attempted, got %v", handler.increased)
	}
}
