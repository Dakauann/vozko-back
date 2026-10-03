package billing_usecase

import (
	"testing"
	"time"

	"vozko/domain/invoice"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

// Reproduces Anhanguera (7fba67b3): an unpaid invoice due 23/09 was left behind
// when the workspace bought a fresh plan on 23/09 running to 23/10. On 27/09 the
// sweep expired the plan they had just paid for.
func staleInvoiceFixture() (*fakeInvoiceRepo, *fakeSubs, *cancelSweepUseCase) {
	due := time.Date(2026, 9, 23, 3, 0, 0, 0, time.UTC)
	stale := unpaidMonthly("inv-old", "ws-anhanguera")
	stale.DueDate = &due

	fresh := &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-new",
		WorkspaceID:        "ws-anhanguera",
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: time.Date(2026, 9, 23, 14, 17, 55, 0, time.UTC),
		CurrentPeriodEnd:   time.Date(2026, 10, 23, 3, 0, 0, 0, time.UTC),
	}

	invoices := &fakeInvoiceRepo{unpaid: []invoice.Invoice{stale}}
	subs := &fakeSubs{latest: fresh}
	uc := NewCancelSweepUseCase(invoices, subs, &fakeAddons{}, &fakeEntitlementHandler{}, &fakeAlerter{})
	uc.now = func() time.Time { return time.Date(2026, 9, 27, 3, 48, 0, 0, time.UTC) }
	return invoices, subs, uc
}

func TestSweep_KeepsSubscriptionBoughtAfterTheUnpaidInvoiceWasDue(t *testing.T) {
	_, subs, uc := staleInvoiceFixture()

	if _, err := uc.Execute(); err != nil {
		t.Fatalf("sweep failed: %v", err)
	}

	if subs.latest.Status == workspace_plan.SubscriptionStatusExpired {
		t.Fatalf(
			"the workspace paid for a new period (%s to %s) after the unpaid invoice fell due (23/09); "+
				"a stale invoice from an earlier period must not expire it",
			subs.latest.CurrentPeriodStart.Format("02/01"),
			subs.latest.CurrentPeriodEnd.Format("02/01"),
		)
	}
}

func TestSweep_StillExpiresTheSubscriptionTheInvoiceBelongsTo(t *testing.T) {
	due := time.Date(2026, 9, 23, 3, 0, 0, 0, time.UTC)
	unpaid := unpaidMonthly("inv-current", "ws-delinquent")
	unpaid.DueDate = &due

	current := &workspace_plan.WorkspaceSubscription{
		ID:                 "sub-current",
		WorkspaceID:        "ws-delinquent",
		Status:             workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: time.Date(2026, 8, 23, 3, 0, 0, 0, time.UTC),
		CurrentPeriodEnd:   time.Date(2026, 9, 23, 3, 0, 0, 0, time.UTC),
	}

	invoices := &fakeInvoiceRepo{unpaid: []invoice.Invoice{unpaid}}
	subs := &fakeSubs{latest: current}
	uc := NewCancelSweepUseCase(invoices, subs, &fakeAddons{}, &fakeEntitlementHandler{}, &fakeAlerter{})
	uc.now = func() time.Time { return time.Date(2026, 9, 27, 3, 48, 0, 0, time.UTC) }

	if _, err := uc.Execute(); err != nil {
		t.Fatalf("sweep failed: %v", err)
	}
	if subs.latest.Status != workspace_plan.SubscriptionStatusExpired {
		t.Fatalf("a genuinely unpaid period must still be swept, got %s", subs.latest.Status)
	}
}
