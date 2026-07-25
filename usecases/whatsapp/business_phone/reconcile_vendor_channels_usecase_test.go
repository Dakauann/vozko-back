package businessphone_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
)

type fakeOpsAlerter struct {
	alerts []string
	err    error
}

func (f *fakeOpsAlerter) Alert(_ context.Context, subject, detail string) error {
	f.alerts = append(f.alerts, subject+"|"+detail)
	return f.err
}

// live builds a partner channel listing entry in the billing state.
func live(id string) businessphone.Dialog360Channel {
	return businessphone.Dialog360Channel{ID: id, PhoneNumber: "+55119" + id, WABAExternalID: "waba-" + id, Status: "live"}
}

func suspendedRef(channelID, clientID, ws string) businessphone.Dialog360ChannelRef {
	return businessphone.Dialog360ChannelRef{Dialog360ChannelID: channelID, Dialog360ClientID: clientID, WorkspaceID: ws, Active: false}
}

func activeRef(channelID, clientID, ws string) businessphone.Dialog360ChannelRef {
	return businessphone.Dialog360ChannelRef{Dialog360ChannelID: channelID, Dialog360ClientID: clientID, WorkspaceID: ws, Active: true}
}

// TestVendorReconcile_RecancelsLeakedChannel: the core leak. The vendor still has a live channel that
// Vozko already suspended (a cancellation that never landed). The pass must re-cancel it and not alert.
func TestVendorReconcile_RecancelsLeakedChannel(t *testing.T) {
	partner := &fakePartnerSvc{channels: []businessphone.Dialog360Channel{live("chA")}}
	reader := &fakeOwnerReader{channelRefs: []businessphone.Dialog360ChannelRef{suspendedRef("chA", "clA", "ws-1")}}
	alerter := &fakeOpsAlerter{}

	uc := NewReconcileVendorChannelsUseCase(partner, reader, alerter)
	report, err := uc.Execute()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if report.VendorBilling != 1 || report.Leaks != 1 || report.Recancelled != 1 || report.Orphans != 0 {
		t.Fatalf("report = %+v, want VendorBilling=1 Leaks=1 Recancelled=1 Orphans=0", report)
	}
	if len(partner.cancelled) != 1 || partner.cancelled[0] != "clA/chA" {
		t.Fatalf("cancelled = %v, want one clA/chA", partner.cancelled)
	}
	if len(alerter.alerts) != 0 {
		t.Fatalf("a re-cancelled leak must NOT alert, got %v", alerter.alerts)
	}
}

// TestVendorReconcile_AlertsOnOrphan: a live vendor channel Vozko has no record of. Vozko cannot cancel
// it (no client id), so it must alert for manual action and never silently leave it billing.
func TestVendorReconcile_AlertsOnOrphan(t *testing.T) {
	partner := &fakePartnerSvc{channels: []businessphone.Dialog360Channel{live("ghost")}}
	reader := &fakeOwnerReader{channelRefs: nil}
	alerter := &fakeOpsAlerter{}

	uc := NewReconcileVendorChannelsUseCase(partner, reader, alerter)
	report, err := uc.Execute()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if report.Orphans != 1 || report.Leaks != 0 || report.Recancelled != 0 {
		t.Fatalf("report = %+v, want Orphans=1 Leaks=0 Recancelled=0", report)
	}
	if len(partner.cancelled) != 0 {
		t.Fatalf("an orphan has no client id, must not be cancelled, got %v", partner.cancelled)
	}
	if len(alerter.alerts) != 1 || !strings.Contains(alerter.alerts[0], "orphan") {
		t.Fatalf("orphan must raise one alert, got %v", alerter.alerts)
	}
}

// TestVendorReconcile_ConsistentActiveChannelNoAction: Vozko considers the channel active and the
// vendor bills it. This is the healthy state. No cancel, no alert.
func TestVendorReconcile_ConsistentActiveChannelNoAction(t *testing.T) {
	partner := &fakePartnerSvc{channels: []businessphone.Dialog360Channel{live("chOK")}}
	reader := &fakeOwnerReader{channelRefs: []businessphone.Dialog360ChannelRef{activeRef("chOK", "clOK", "ws-1")}}
	alerter := &fakeOpsAlerter{}

	uc := NewReconcileVendorChannelsUseCase(partner, reader, alerter)
	report, err := uc.Execute()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if report.VendorBilling != 1 || report.Leaks != 0 || report.Orphans != 0 || report.Recancelled != 0 {
		t.Fatalf("report = %+v, want VendorBilling=1 and zero divergence", report)
	}
	if len(partner.cancelled) != 0 || len(alerter.alerts) != 0 {
		t.Fatalf("consistent channel must be untouched; cancelled=%v alerts=%v", partner.cancelled, alerter.alerts)
	}
}

// TestVendorReconcile_IgnoresAlreadyCancelledChannel: a channel Vozko suspended that the vendor already
// shows as pending_deletion is consistent (the cancel landed). It is not billing next month, so the
// pass must NOT count it or re-cancel it.
func TestVendorReconcile_IgnoresAlreadyCancelledChannel(t *testing.T) {
	partner := &fakePartnerSvc{channels: []businessphone.Dialog360Channel{
		{ID: "chGone", Status: "pending_deletion"},
	}}
	reader := &fakeOwnerReader{channelRefs: []businessphone.Dialog360ChannelRef{suspendedRef("chGone", "clG", "ws-1")}}
	alerter := &fakeOpsAlerter{}

	uc := NewReconcileVendorChannelsUseCase(partner, reader, alerter)
	report, err := uc.Execute()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if report.VendorBilling != 0 || report.Leaks != 0 {
		t.Fatalf("report = %+v, want VendorBilling=0 Leaks=0 for a pending_deletion channel", report)
	}
	if len(partner.cancelled) != 0 || len(alerter.alerts) != 0 {
		t.Fatalf("an already-cancelled channel must be untouched; cancelled=%v alerts=%v", partner.cancelled, alerter.alerts)
	}
}

// TestVendorReconcile_AlertsWhenRecancelFails: the cancellation re-submit errors at the vendor. The
// leak is counted but NOT marked recancelled, and an ops alert is raised so a human resolves it.
func TestVendorReconcile_AlertsWhenRecancelFails(t *testing.T) {
	partner := &fakePartnerSvc{channels: []businessphone.Dialog360Channel{live("chA")}, cancelErr: errors.New("vendor 500")}
	reader := &fakeOwnerReader{channelRefs: []businessphone.Dialog360ChannelRef{suspendedRef("chA", "clA", "ws-1")}}
	alerter := &fakeOpsAlerter{}

	uc := NewReconcileVendorChannelsUseCase(partner, reader, alerter)
	report, err := uc.Execute()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if report.Leaks != 1 || report.Recancelled != 0 {
		t.Fatalf("report = %+v, want Leaks=1 Recancelled=0", report)
	}
	if len(alerter.alerts) != 1 || !strings.Contains(alerter.alerts[0], "cancellation lost") {
		t.Fatalf("a failed re-cancel must alert, got %v", alerter.alerts)
	}
}

// TestVendorReconcile_AlertsWhenClientIDMissing: a leaked channel with no client id cannot be cancelled
// from here at all (the WABA join carried no client id), so it must alert rather than attempt a cancel.
func TestVendorReconcile_AlertsWhenClientIDMissing(t *testing.T) {
	partner := &fakePartnerSvc{channels: []businessphone.Dialog360Channel{live("chA")}}
	reader := &fakeOwnerReader{channelRefs: []businessphone.Dialog360ChannelRef{suspendedRef("chA", "", "ws-1")}}
	alerter := &fakeOpsAlerter{}

	uc := NewReconcileVendorChannelsUseCase(partner, reader, alerter)
	report, err := uc.Execute()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if report.Leaks != 1 || report.Recancelled != 0 {
		t.Fatalf("report = %+v, want Leaks=1 Recancelled=0", report)
	}
	if len(partner.cancelled) != 0 {
		t.Fatalf("a missing client id must not attempt a cancel, got %v", partner.cancelled)
	}
	if len(alerter.alerts) != 1 {
		t.Fatalf("a non-cancellable leak must alert, got %v", alerter.alerts)
	}
}

// TestVendorReconcile_MixedFleetCounts drives every branch in one pass and asserts the aggregate report.
func TestVendorReconcile_MixedFleetCounts(t *testing.T) {
	partner := &fakePartnerSvc{channels: []businessphone.Dialog360Channel{
		live("ok"),     // consistent active
		live("leak"),   // suspended in Vozko -> re-cancel
		live("ghost"),  // no Vozko record -> orphan
		{ID: "gone", Status: "pending_deletion"}, // not billing -> ignored
	}}
	reader := &fakeOwnerReader{channelRefs: []businessphone.Dialog360ChannelRef{
		activeRef("ok", "clOK", "ws-1"),
		suspendedRef("leak", "clLeak", "ws-2"),
		suspendedRef("gone", "clGone", "ws-3"),
	}}
	alerter := &fakeOpsAlerter{}

	uc := NewReconcileVendorChannelsUseCase(partner, reader, alerter)
	report, err := uc.Execute()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if report.VendorBilling != 3 || report.Orphans != 1 || report.Leaks != 1 || report.Recancelled != 1 {
		t.Fatalf("report = %+v, want VendorBilling=3 Orphans=1 Leaks=1 Recancelled=1", report)
	}
	if len(partner.cancelled) != 1 || partner.cancelled[0] != "clLeak/leak" {
		t.Fatalf("only the leak should be cancelled, got %v", partner.cancelled)
	}
	if len(alerter.alerts) != 1 || !strings.Contains(alerter.alerts[0], "orphan") {
		t.Fatalf("only the orphan should alert, got %v", alerter.alerts)
	}
}

func TestVendorReconcile_PropagatesListChannelsError(t *testing.T) {
	partner := &fakePartnerSvc{listErr: errors.New("partner down")}
	uc := NewReconcileVendorChannelsUseCase(partner, &fakeOwnerReader{}, &fakeOpsAlerter{})
	if _, err := uc.Execute(); err == nil {
		t.Fatal("a partner listing failure must surface as an error, not a silent empty pass")
	}
}

func TestVendorReconcile_PropagatesVozkoListError(t *testing.T) {
	partner := &fakePartnerSvc{channels: []businessphone.Dialog360Channel{live("chA")}}
	reader := &fakeOwnerReader{listErr: errors.New("db down")}
	uc := NewReconcileVendorChannelsUseCase(partner, reader, &fakeOpsAlerter{})
	if _, err := uc.Execute(); err == nil {
		t.Fatal("a Vozko read failure must surface as an error (never cancel on uncertain data)")
	}
}

// TestVendorReconcile_NilAlerterDoesNotPanic: alerting is best-effort. A pass with no alerter sink
// wired must still detect divergence and never panic.
func TestVendorReconcile_NilAlerterDoesNotPanic(t *testing.T) {
	partner := &fakePartnerSvc{channels: []businessphone.Dialog360Channel{live("ghost")}}
	uc := NewReconcileVendorChannelsUseCase(partner, &fakeOwnerReader{}, nil)
	report, err := uc.Execute()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if report.Orphans != 1 {
		t.Fatalf("report = %+v, want Orphans=1 even with no alerter", report)
	}
}

// TestVendorReconcile_AlertSinkErrorIsSwallowed: if the ops-alert sink itself fails, the pass must not
// fail (a broken alerter must never block the reconcile loop).
func TestVendorReconcile_AlertSinkErrorIsSwallowed(t *testing.T) {
	partner := &fakePartnerSvc{channels: []businessphone.Dialog360Channel{live("ghost")}}
	alerter := &fakeOpsAlerter{err: errors.New("sink down")}
	uc := NewReconcileVendorChannelsUseCase(partner, &fakeOwnerReader{}, alerter)
	report, err := uc.Execute()
	if err != nil {
		t.Fatalf("a failing alert sink must not fail the pass: %v", err)
	}
	if report.Orphans != 1 || len(alerter.alerts) != 1 {
		t.Fatalf("report = %+v alerts=%v, want Orphans=1 and the alert attempted", report, alerter.alerts)
	}
}

func TestVendorIsBilling(t *testing.T) {
	billingStates := []string{"live", "LIVE", "Live", "running", "weird-unknown"}
	for _, s := range billingStates {
		if !vendorIsBilling(s) {
			t.Errorf("status %q should count as billing (unknown defaults to billing, fail-safe)", s)
		}
	}
	nonBilling := []string{"", "pending", "draft", "sandbox", "unregistered", "pending_deletion", "deleted", "cancelled", "canceled", "terminated", "inactive", "banned", "  Pending_Deletion  "}
	for _, s := range nonBilling {
		if vendorIsBilling(s) {
			t.Errorf("status %q should NOT count as billing", s)
		}
	}
}
