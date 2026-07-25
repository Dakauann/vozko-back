package billing_usecase

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"vozko/domain/invoice"
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type fakeInvoiceRepo struct {
	invoice.Repository
	unpaid   []invoice.Invoice
	err      error
	statuses map[string]invoice.Status
}

func (f *fakeInvoiceRepo) ListUnpaidByPurpose(_ invoice.Purpose, afterID string, limit int) ([]invoice.Invoice, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []invoice.Invoice
	for _, inv := range f.unpaid {
		if f.statuses[inv.ID] == invoice.StatusExpired {
			continue // already swept; mimics the "status IN (PENDING, OVERDUE)" filter
		}
		if inv.ID > afterID {
			out = append(out, inv)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeInvoiceRepo) UpdateStatus(id string, s invoice.Status) error {
	if f.statuses == nil {
		f.statuses = map[string]invoice.Status{}
	}
	f.statuses[id] = s
	return nil
}

type fakeEntitlementHandler struct {
	reduced   []string
	increased []string
	failWS    map[string]error
}

func (f *fakeEntitlementHandler) OnEntitlementReduced(ws string, _ workspace_addon.EntitlementKind) error {
	f.reduced = append(f.reduced, ws)
	if f.failWS != nil {
		if err := f.failWS[ws]; err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeEntitlementHandler) OnEntitlementIncreased(ws string, _ workspace_addon.EntitlementKind) error {
	f.increased = append(f.increased, ws)
	if f.failWS != nil {
		if err := f.failWS[ws]; err != nil {
			return err
		}
	}
	return nil
}

type fakeAlerter struct{ calls int }

func (f *fakeAlerter) Alert(context.Context, string, string) error { f.calls++; return nil }

func unpaidMonthly(id, ws string) invoice.Invoice {
	return invoice.Invoice{ID: id, WorkspaceID: ws, Purpose: invoice.PurposeMonthlyBilling, Status: invoice.StatusOverdue}
}

func sweepFixture(inv *fakeInvoiceRepo, subs *fakeSubs, addons *fakeAddons, onReduced *fakeEntitlementHandler, alerter *fakeAlerter, day int) *cancelSweepUseCase {
	uc := NewCancelSweepUseCase(inv, subs, addons, onReduced, alerter)
	uc.now = func() time.Time { return time.Date(2026, 3, day, 12, 0, 0, 0, time.UTC) }
	return uc
}

func TestSweep_BeforeCutoffDoesNothing(t *testing.T) {
	inv := &fakeInvoiceRepo{unpaid: []invoice.Invoice{unpaidMonthly("inv-1", "ws-1")}}
	onReduced := &fakeEntitlementHandler{}
	n, err := sweepFixture(inv, &fakeSubs{}, &fakeAddons{}, onReduced, &fakeAlerter{}, 20).Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 0 || len(onReduced.reduced) != 0 {
		t.Fatalf("before the cutoff the sweep must not cancel anything, swept=%d reduced=%v", n, onReduced.reduced)
	}
}

func TestSweep_CancelsUnpaidWorkspace(t *testing.T) {
	inv := &fakeInvoiceRepo{unpaid: []invoice.Invoice{unpaidMonthly("inv-1", "ws-1")}}
	subs := &fakeSubs{latest: planSub("ws-1", time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC))}
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{
		"ws-1": {channelAddon(25_000_000, 1)},
	}}
	onReduced := &fakeEntitlementHandler{}
	alerter := &fakeAlerter{}

	n, err := sweepFixture(inv, subs, addons, onReduced, alerter, 27).Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected one workspace swept, got %d", n)
	}
	if len(addons.updated) != 1 || addons.updated[0].Status != workspace_plan.SubscriptionStatusExpired {
		t.Fatalf("the channel addon should be lapsed, got %+v", addons.updated)
	}
	if len(subs.updated) != 1 || subs.updated[0].Status != workspace_plan.SubscriptionStatusExpired {
		t.Fatalf("the plan subscription should be expired, got %+v", subs.updated)
	}
	if len(onReduced.reduced) != 1 || onReduced.reduced[0] != "ws-1" {
		t.Fatalf("OnEntitlementReduced should cancel ws-1's channels, got %v", onReduced.reduced)
	}
	if inv.statuses["inv-1"] != invoice.StatusExpired {
		t.Fatalf("the invoice should be marked expired after a confirmed cancellation, got %q", inv.statuses["inv-1"])
	}
	if alerter.calls != 0 {
		t.Fatalf("a successful sweep should not alert, got %d", alerter.calls)
	}
}

func TestSweep_CancellationFailureAlertsAndRetries(t *testing.T) {
	inv := &fakeInvoiceRepo{unpaid: []invoice.Invoice{unpaidMonthly("inv-1", "ws-1")}}
	subs := &fakeSubs{latest: planSub("ws-1", time.Now())}
	addons := &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{"ws-1": {channelAddon(25_000_000, 1)}}}
	onReduced := &fakeEntitlementHandler{failWS: map[string]error{"ws-1": errors.New("vendor down")}}
	alerter := &fakeAlerter{}

	n, err := sweepFixture(inv, subs, addons, onReduced, alerter, 27).Execute()
	if err != nil {
		t.Fatalf("a per-workspace cancel failure must not fail the whole run: %v", err)
	}
	if n != 0 {
		t.Fatalf("a failed cancellation must not count as swept, got %d", n)
	}
	if inv.statuses["inv-1"] == invoice.StatusExpired {
		t.Fatal("the invoice must stay unpaid so the next sweep retries it")
	}
	if alerter.calls != 1 {
		t.Fatalf("an unconfirmed cancellation must raise exactly one ops alert, got %d", alerter.calls)
	}
}

func TestSweep_KeysetPaginationSweepsEveryWorkspace(t *testing.T) {
	var unpaid []invoice.Invoice
	byWS := map[string][]*workspace_addon.AddonSubscription{}
	for _, n := range []string{"1", "2", "3", "4", "5"} {
		unpaid = append(unpaid, unpaidMonthly("inv-"+n, "ws-"+n))
		byWS["ws-"+n] = []*workspace_addon.AddonSubscription{channelAddon(25_000_000, 1)}
	}
	inv := &fakeInvoiceRepo{unpaid: unpaid}
	onReduced := &fakeEntitlementHandler{}
	uc := sweepFixture(inv, &fakeSubs{}, &fakeAddons{byWS: byWS}, onReduced, &fakeAlerter{}, 28)
	uc.batchSize = 2

	n, err := uc.Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 5 || len(onReduced.reduced) != 5 {
		t.Fatalf("keyset pagination should sweep all five, swept=%d reduced=%d", n, len(onReduced.reduced))
	}
}

func TestSweep_OneFailureDoesNotBlockOthers(t *testing.T) {
	inv := &fakeInvoiceRepo{unpaid: []invoice.Invoice{
		unpaidMonthly("inv-1", "ws-1"),
		unpaidMonthly("inv-2", "ws-2"),
		unpaidMonthly("inv-3", "ws-3"),
	}}
	byWS := map[string][]*workspace_addon.AddonSubscription{
		"ws-1": {channelAddon(25_000_000, 1)},
		"ws-2": {channelAddon(25_000_000, 1)},
		"ws-3": {channelAddon(25_000_000, 1)},
	}
	onReduced := &fakeEntitlementHandler{failWS: map[string]error{"ws-2": errors.New("vendor blip")}}
	alerter := &fakeAlerter{}

	n, err := sweepFixture(inv, &fakeSubs{}, &fakeAddons{byWS: byWS}, onReduced, alerter, 27).Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 2 {
		t.Fatalf("ws-1 and ws-3 should be swept despite ws-2 failing, got %d", n)
	}
	if inv.statuses["inv-2"] == invoice.StatusExpired {
		t.Fatal("ws-2's invoice must stay unpaid for retry")
	}
	if inv.statuses["inv-1"] != invoice.StatusExpired || inv.statuses["inv-3"] != invoice.StatusExpired {
		t.Fatal("ws-1 and ws-3 invoices should be marked expired")
	}
	if alerter.calls != 1 {
		t.Fatalf("only ws-2 should alert, got %d", alerter.calls)
	}
}

func TestSweep_InvoiceRepoErrorPropagates(t *testing.T) {
	inv := &fakeInvoiceRepo{err: errors.New("db down")}
	if _, err := sweepFixture(inv, &fakeSubs{}, &fakeAddons{}, &fakeEntitlementHandler{}, &fakeAlerter{}, 27).Execute(); err == nil {
		t.Fatal("expected the unpaid-invoice query error to propagate")
	}
}
