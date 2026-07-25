package businessphone_usecase

import (
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
)

// THE FIX: a 360dialog channel that is live at the vendor and CONNECTED at Vozko but owned by no
// workspace bills for nobody and can never be reactivated (reactivation is owner-keyed). The vendor
// reconciler must cancel it — otherwise an unassigned live channel bills forever.
func TestVendorReconcile_CancelsOwnerlessLiveChannel(t *testing.T) {
	partner := &fakePartnerSvc{channels: []businessphone.Dialog360Channel{live("chO")}}
	reader := &fakeOwnerReader{channelRefs: []businessphone.Dialog360ChannelRef{activeRef("chO", "clO", "")}}
	alerter := &fakeOpsAlerter{}

	report, err := NewReconcileVendorChannelsUseCase(partner, reader, alerter).Execute()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if report.Ownerless != 1 || report.Recancelled != 1 || report.Leaks != 0 || report.Orphans != 0 {
		t.Fatalf("report = %+v, want Ownerless=1 Recancelled=1 Leaks=0 Orphans=0", report)
	}
	if len(partner.cancelled) != 1 || partner.cancelled[0] != "clO/chO" {
		t.Fatalf("cancelled = %v, want one clO/chO (client-scoped, owner-independent)", partner.cancelled)
	}
	if len(alerter.alerts) != 0 {
		t.Fatalf("a re-cancelled ownerless channel must NOT alert, got %v", alerter.alerts)
	}
}

// An ownerless live channel whose WABA (client id) is gone cannot be auto-cancelled — it must alert
// for manual action, never silently keep billing.
func TestVendorReconcile_OwnerlessMissingClientID_Alerts(t *testing.T) {
	partner := &fakePartnerSvc{channels: []businessphone.Dialog360Channel{live("chO")}}
	reader := &fakeOwnerReader{channelRefs: []businessphone.Dialog360ChannelRef{activeRef("chO", "", "")}}
	alerter := &fakeOpsAlerter{}

	report, err := NewReconcileVendorChannelsUseCase(partner, reader, alerter).Execute()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if report.Ownerless != 1 || report.Recancelled != 0 {
		t.Fatalf("report = %+v, want Ownerless=1 Recancelled=0", report)
	}
	if len(partner.cancelled) != 0 {
		t.Fatalf("a missing client id must not attempt a cancel, got %v", partner.cancelled)
	}
	if len(alerter.alerts) != 1 {
		t.Fatalf("an un-cancellable ownerless channel must alert, got %v", alerter.alerts)
	}
}

// An OWNED active channel remains consistent (untouched) — the ownerless case must not over-trigger.
func TestVendorReconcile_OwnedActiveStaysConsistent(t *testing.T) {
	partner := &fakePartnerSvc{channels: []businessphone.Dialog360Channel{live("chO")}}
	reader := &fakeOwnerReader{channelRefs: []businessphone.Dialog360ChannelRef{activeRef("chO", "clO", "ws-1")}}
	alerter := &fakeOpsAlerter{}

	report, err := NewReconcileVendorChannelsUseCase(partner, reader, alerter).Execute()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if report.Ownerless != 0 || report.Recancelled != 0 || report.Leaks != 0 {
		t.Fatalf("report = %+v, want everything 0 (consistent owned channel)", report)
	}
	if len(partner.cancelled) != 0 || len(alerter.alerts) != 0 {
		t.Fatalf("an owned active channel must be untouched; cancelled=%v alerts=%v", partner.cancelled, alerter.alerts)
	}
}
