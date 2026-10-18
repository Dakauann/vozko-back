package billing_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/invoice"
	"vozko/domain/workspace"
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type recordingNotices struct {
	issued []string
	near   []string
	cut    []string
	err    error
}

func (r *recordingNotices) InvoiceIssued(inv *invoice.Invoice) error {
	r.issued = append(r.issued, inv.ID)
	return r.err
}

func (r *recordingNotices) CutoffNear(inv *invoice.Invoice, _ time.Time) error {
	r.near = append(r.near, inv.ID)
	return r.err
}

func (r *recordingNotices) AccessCut(inv *invoice.Invoice) error {
	r.cut = append(r.cut, inv.ID)
	return r.err
}

func dueMonthly(id, ws string, due time.Time) invoice.Invoice {
	inv := unpaidMonthly(id, ws)
	inv.DueDate = &due
	return inv
}

func march(day int) time.Time { return time.Date(2026, 3, day, 12, 0, 0, 0, time.UTC) }

func sweepWithNotices(inv *fakeInvoiceRepo, subs *fakeSubs, notices *recordingNotices, day int) *cancelSweepUseCase {
	uc := NewCancelSweepUseCase(inv, subs, &fakeAddons{}, &fakeEntitlementHandler{}, &fakeAlerter{}, notices)
	uc.now = func() time.Time { return march(day) }
	return uc
}

func TestAnIssuedInvoiceIsAnnounced(t *testing.T) {
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{activeSub("ws-1", "plan-1")}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", BasePriceBRLCents: 109_900}}}
	wss := &fakeWorkspaces{byWS: map[string]*workspace.Workspace{"ws-1": owner("ws-1", "user-1")}}
	notices := &recordingNotices{}
	uc := NewEmitMonthlyInvoicesUseCase(subs, plans, &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{}}, wss, &fakePricing{rate: 6.0}, &fakeCreateInvoice{}, notices)
	uc.now = func() time.Time { return march(18) }

	if n, err := uc.Execute(); err != nil || n != 1 {
		t.Fatalf("emitted %d, err %v", n, err)
	}
	if len(notices.issued) != 1 {
		t.Fatalf("issued notices = %v", notices.issued)
	}
}

func TestAFailedAnnouncementKeepsTheInvoice(t *testing.T) {
	subs := &fakeSubs{subs: []*workspace_plan.WorkspaceSubscription{activeSub("ws-1", "plan-1")}}
	plans := &fakePlans{plans: map[string]*workspace_plan.PlanDefinition{"plan-1": {ID: "plan-1", BasePriceBRLCents: 109_900}}}
	wss := &fakeWorkspaces{byWS: map[string]*workspace.Workspace{"ws-1": owner("ws-1", "user-1")}}
	created := &fakeCreateInvoice{}
	uc := NewEmitMonthlyInvoicesUseCase(subs, plans, &fakeAddons{byWS: map[string][]*workspace_addon.AddonSubscription{}}, wss, &fakePricing{rate: 6.0}, created, &recordingNotices{err: errors.New("mail down")})
	uc.now = func() time.Time { return march(18) }

	if n, err := uc.Execute(); err != nil || n != 1 || len(created.calls) != 1 {
		t.Fatalf("emitted %d, calls %d, err %v", n, len(created.calls), err)
	}
}

func TestAnOverdueInvoiceIsWarnedBeforeTheCutoff(t *testing.T) {
	inv := &fakeInvoiceRepo{unpaid: []invoice.Invoice{dueMonthly("inv-1", "ws-1", time.Date(2026, 3, 23, 3, 0, 0, 0, time.UTC))}}
	subs := &fakeSubs{latest: planSub("ws-1", time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC))}
	notices := &recordingNotices{}

	if _, err := sweepWithNotices(inv, subs, notices, 25).Execute(); err != nil {
		t.Fatal(err)
	}

	if len(notices.near) != 1 || notices.near[0] != "inv-1" || len(notices.cut) != 0 || len(subs.updated) != 0 {
		t.Fatalf("near %v cut %v updated %v", notices.near, notices.cut, subs.updated)
	}
}

func TestNoWarningBeforeTheInvoiceIsOverdue(t *testing.T) {
	inv := &fakeInvoiceRepo{unpaid: []invoice.Invoice{dueMonthly("inv-1", "ws-1", time.Date(2026, 3, 23, 3, 0, 0, 0, time.UTC))}}
	notices := &recordingNotices{}

	if _, err := sweepWithNotices(inv, &fakeSubs{}, notices, 20).Execute(); err != nil {
		t.Fatal(err)
	}

	if len(notices.near) != 0 || len(notices.cut) != 0 {
		t.Fatalf("near %v cut %v", notices.near, notices.cut)
	}
}

func TestCuttingAccessIsAnnounced(t *testing.T) {
	inv := &fakeInvoiceRepo{unpaid: []invoice.Invoice{dueMonthly("inv-1", "ws-1", time.Date(2026, 3, 23, 3, 0, 0, 0, time.UTC))}}
	subs := &fakeSubs{latest: planSub("ws-1", time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC))}
	notices := &recordingNotices{}

	if n, err := sweepWithNotices(inv, subs, notices, 28).Execute(); err != nil || n != 1 {
		t.Fatalf("swept %d, err %v", n, err)
	}

	if len(notices.cut) != 1 || notices.cut[0] != "inv-1" || len(notices.near) != 0 {
		t.Fatalf("cut %v near %v", notices.cut, notices.near)
	}
}

func TestAnInvoiceOfAPeriodAlreadyReplacedIsNeitherWarnedNorAnnouncedAsCut(t *testing.T) {
	due := time.Date(2026, 3, 23, 3, 0, 0, 0, time.UTC)
	fresh := &workspace_plan.WorkspaceSubscription{ID: "sub-new", WorkspaceID: "ws-1", Status: workspace_plan.SubscriptionStatusActive,
		CurrentPeriodStart: time.Date(2026, 3, 23, 14, 0, 0, 0, time.UTC), CurrentPeriodEnd: time.Date(2026, 4, 23, 3, 0, 0, 0, time.UTC)}
	for _, day := range []int{25, 28} {
		inv := &fakeInvoiceRepo{unpaid: []invoice.Invoice{dueMonthly("inv-old", "ws-1", due)}}
		notices := &recordingNotices{}

		if _, err := sweepWithNotices(inv, &fakeSubs{latest: fresh}, notices, day).Execute(); err != nil {
			t.Fatal(err)
		}

		if len(notices.near) != 0 || len(notices.cut) != 0 {
			t.Fatalf("day %d: near %v cut %v", day, notices.near, notices.cut)
		}
	}
}
