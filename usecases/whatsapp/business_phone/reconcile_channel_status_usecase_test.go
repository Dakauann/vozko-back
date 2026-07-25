package businessphone_usecase

import (
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
)

type fakeChannelPartner struct {
	businessphone.Dialog360PartnerService
	channels []businessphone.Dialog360Channel
	fresh    map[string]businessphone.Dialog360Channel // single-channel reads (GetChannel)
	getCalls int
}

func (f *fakeChannelPartner) ListChannels() ([]businessphone.Dialog360Channel, error) {
	return f.channels, nil
}

func (f *fakeChannelPartner) GetChannel(id string) (*businessphone.Dialog360Channel, error) {
	f.getCalls++
	if ch, ok := f.fresh[id]; ok {
		return &ch, nil
	}
	return nil, nil
}

type fakeRefsReader struct {
	businessphone.OwnerPhoneReader
	refs []businessphone.Dialog360ChannelRef
}

func (f *fakeRefsReader) ListDialog360ChannelRefs() ([]businessphone.Dialog360ChannelRef, error) {
	return f.refs, nil
}

// TestReconcileChannelStatus_BackfillsAndSuspends is the core proof: a live channel's
// lagged metadata (number/name/quality/tier) gets backfilled, the WABA name is filled,
// and a channel deactivated at 360dialog flips the local row to SUSPENDED.
func TestReconcileChannelStatus_BackfillsAndSuspends(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()

	repo.phoneNumbers["p-live"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID: "p-live", Provider: businessphone.ProviderDialog360,
		Status: businessphone.StatusConnected, WABAId: "wabaLive", Dialog360ChannelID: "chLive",
	}
	repo.phoneNumbers["p-dead"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID: "p-dead", Provider: businessphone.ProviderDialog360,
		Status: businessphone.StatusConnected, WABAId: "wabaDead", Dialog360ChannelID: "chDead",
	}
	w := seedWABA(wabaRepo, "waba-internal", "wabaLive")
	w.Name = "" // ensure blank so the name backfill applies

	partner := &fakeChannelPartner{channels: []businessphone.Dialog360Channel{
		{
			ID: "chLive", WABAExternalID: "wabaLive", PhoneNumber: "15553395514",
			PhoneName: "Vozko Relacionamentos", QualityRating: "GREEN", MessagingTier: "TIER_1K",
			ReviewStatus: "approved", WABAName: "Vozko Rel Ltda", HubStatus: "live",
		},
		{ID: "chDead", WABAExternalID: "wabaDead", HubStatus: "pending_deletion", Cancelled: true},
	}}
	reader := &fakeRefsReader{refs: []businessphone.Dialog360ChannelRef{
		{PhoneID: "p-live", Dialog360ChannelID: "chLive", Active: true},
		{PhoneID: "p-dead", Dialog360ChannelID: "chDead", Active: true},
	}}

	report, err := NewReconcileChannelStatusUseCase(partner, reader, repo, wabaRepo).Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	live := repo.phoneNumbers["p-live"]
	if live.DisplayPhoneNumber != "15553395514" {
		t.Fatalf("display number not backfilled: %q", live.DisplayPhoneNumber)
	}
	if live.VerifiedName != "Vozko Relacionamentos" {
		t.Fatalf("verified name not backfilled: %q", live.VerifiedName)
	}
	if string(live.QualityRating) != "GREEN" {
		t.Fatalf("quality not backfilled: %q", live.QualityRating)
	}
	if live.MessagingLimitTier != "TIER_1K" {
		t.Fatalf("tier not backfilled: %q", live.MessagingLimitTier)
	}
	if live.AccountReviewStatus != "approved" {
		t.Fatalf("review status not backfilled: %q", live.AccountReviewStatus)
	}
	if live.Status != businessphone.StatusConnected {
		t.Fatalf("a live channel must stay CONNECTED, got %s", live.Status)
	}

	dead := repo.phoneNumbers["p-dead"]
	if dead.Status != businessphone.StatusSuspended {
		t.Fatalf("a deactivated channel must SUSPEND locally, got %s", dead.Status)
	}

	if rec, _ := wabaRepo.FindByMetaWABAId("wabaLive"); rec == nil || rec.Name != "Vozko Rel Ltda" {
		t.Fatalf("WABA name not backfilled: %+v", rec)
	}
	if report.Updated < 1 || report.Suspended != 1 || report.WABAsNamed != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	// The bulk list already carried the number and WABA name, so no wasted
	// single-channel reads should have fired.
	if partner.getCalls != 0 {
		t.Fatalf("expected no fresh GetChannel when the bulk list is complete, got %d", partner.getCalls)
	}
}

// TestReconcileChannelStatus_FillsNumberFromFreshReadWhenBulkListLags is the regression
// test for the ground-truth bug seen in production: 360dialog's BULK channel listing
// exposes phone_name before phone_number, so a reconcile pass backfilled the name/tier
// but left the display number blank, needing another full tick. The reconcile must
// close that gap in one pass by re-reading the single lagging channel (which is fresh).
func TestReconcileChannelStatus_FillsNumberFromFreshReadWhenBulkListLags(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	repo.phoneNumbers["p1"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID: "p1", Provider: businessphone.ProviderDialog360, Status: businessphone.StatusConnected,
		WABAId: "wabaLive", Dialog360ChannelID: "chLive", VerifiedName: "Vozko Relacionamentos",
	}

	partner := &fakeChannelPartner{
		// Bulk list: name + tier present, but phone_number still blank (the real lag).
		channels: []businessphone.Dialog360Channel{
			{ID: "chLive", WABAExternalID: "wabaLive", PhoneName: "Vozko Relacionamentos", MessagingTier: "TIER_0.25K", HubStatus: "live"},
		},
		// Fresh single-channel read has the number.
		fresh: map[string]businessphone.Dialog360Channel{
			"chLive": {ID: "chLive", WABAExternalID: "wabaLive", PhoneNumber: "15553395514", PhoneName: "Vozko Relacionamentos", MessagingTier: "TIER_0.25K", HubStatus: "live"},
		},
	}
	reader := &fakeRefsReader{refs: []businessphone.Dialog360ChannelRef{
		{PhoneID: "p1", Dialog360ChannelID: "chLive", Active: true},
	}}

	report, err := NewReconcileChannelStatusUseCase(partner, reader, repo, wabaRepo).Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := repo.phoneNumbers["p1"].DisplayPhoneNumber; got != "15553395514" {
		t.Fatalf("number must be filled from the fresh read despite the bulk-list lag, got %q", got)
	}
	if partner.getCalls != 1 {
		t.Fatalf("expected exactly one fresh GetChannel for the lagging channel, got %d", partner.getCalls)
	}
	if report.Updated < 1 {
		t.Fatalf("expected the row to be updated, got %+v", report)
	}
}

// A channel that is entirely gone from the partner listing must also SUSPEND the row.
func TestReconcileChannelStatus_SuspendsWhenChannelGone(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["p1"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID: "p1", Provider: businessphone.ProviderDialog360,
		Status: businessphone.StatusConnected, Dialog360ChannelID: "chGone",
	}
	partner := &fakeChannelPartner{channels: nil} // channel not present at the vendor
	reader := &fakeRefsReader{refs: []businessphone.Dialog360ChannelRef{
		{PhoneID: "p1", Dialog360ChannelID: "chGone", Active: true},
	}}
	if _, err := NewReconcileChannelStatusUseCase(partner, reader, repo, newMockWABARepo()).Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.phoneNumbers["p1"].Status != businessphone.StatusSuspended {
		t.Fatalf("a channel gone from 360dialog must SUSPEND locally, got %s", repo.phoneNumbers["p1"].Status)
	}
}

// Idempotency: a second pass over an already-correct fleet changes nothing.
func TestReconcileChannelStatus_Idempotent(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["p1"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID: "p1", Provider: businessphone.ProviderDialog360, Status: businessphone.StatusConnected,
		WABAId: "wabaLive", Dialog360ChannelID: "chLive", DisplayPhoneNumber: "15553395514",
		VerifiedName: "Vozko", MessagingLimitTier: "TIER_1K",
	}
	partner := &fakeChannelPartner{channels: []businessphone.Dialog360Channel{
		{ID: "chLive", WABAExternalID: "wabaLive", PhoneNumber: "15553395514", PhoneName: "Vozko", MessagingTier: "TIER_1K", HubStatus: "live"},
	}}
	reader := &fakeRefsReader{refs: []businessphone.Dialog360ChannelRef{{PhoneID: "p1", Dialog360ChannelID: "chLive", Active: true}}}
	uc := NewReconcileChannelStatusUseCase(partner, reader, repo, newMockWABARepo())
	report, _ := uc.Execute()
	if report.Updated != 0 || report.Suspended != 0 {
		t.Fatalf("already-synced fleet must be a no-op, got %+v", report)
	}
}
