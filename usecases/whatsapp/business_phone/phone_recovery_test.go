package businessphone_usecase

import (
	"sort"
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
	workspace_addon "vozko/domain/workspace/workspace_addon"
)

type recordingPhoneNotices struct {
	notified []string
	cleared  []string
}

func (r *recordingPhoneNotices) NotifyPhone(phone *businessphone.WhatsAppBusinessPhoneNumber, notice businessphone.PhoneNotice) error {
	r.notified = append(r.notified, phone.ID+"/"+notice.State)
	return nil
}

func (r *recordingPhoneNotices) ClearPhone(phone *businessphone.WhatsAppBusinessPhoneNumber, states ...string) error {
	for _, state := range states {
		r.cleared = append(r.cleared, phone.ID+"/"+state)
	}
	return nil
}

func (r *recordingPhoneNotices) clearedSorted() []string {
	out := append([]string(nil), r.cleared...)
	sort.Strings(out)
	return out
}

func qualityPayload(event string) *businessphone.PhoneWebhookPayload {
	return &businessphone.PhoneWebhookPayload{Entry: []businessphone.PhoneWebhookEntry{{ID: "waba1", Changes: []businessphone.PhoneWebhookChange{{
		Field: businessphone.FieldPhoneNumberQualityUpdate,
		Value: businessphone.PhoneWebhookValue{DisplayPhoneNumber: "+15551234567", Event: event},
	}}}}}
}

func TestARecoveredQualityClosesTheQualityAlertsOfThatNumber(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551234567", "waba1").Status = businessphone.StatusRestricted
	notices := &recordingPhoneNotices{}
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo(), &fakeQualityRecorder{}, &fakeOptOutRecorder{}).WithNotices(notices)

	if err := uc.Execute(qualityPayload("UNFLAGGED")); err != nil {
		t.Fatal(err)
	}
	got := notices.clearedSorted()
	if len(got) != 2 || got[0] != "p1/quality:FLAGGED" || got[1] != "p1/quality:RESTRICTED" {
		t.Fatalf("cleared %v", got)
	}
	if len(notices.notified) != 0 {
		t.Fatalf("a recovery sends nothing, notified %v", notices.notified)
	}
}

func TestAReinstatedAccountClosesTheBanAlertsOfEveryRestoredNumber(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551234567", "waba1").Status = businessphone.StatusBanned
	seedPhone(repo, "p2", "+15559999999", "waba1").Status = businessphone.StatusFlagged
	notices := &recordingPhoneNotices{}
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo(), &fakeQualityRecorder{}, &fakeOptOutRecorder{}).WithNotices(notices)

	payload := &businessphone.PhoneWebhookPayload{Entry: []businessphone.PhoneWebhookEntry{{ID: "waba1", Changes: []businessphone.PhoneWebhookChange{{
		Field: businessphone.FieldAccountUpdate,
		Value: businessphone.PhoneWebhookValue{BanInfo: &businessphone.BanInfo{WABABanState: "REINSTATE"}},
	}}}}}
	if err := uc.Execute(payload); err != nil {
		t.Fatal(err)
	}
	cleared := map[string]bool{}
	for _, c := range notices.cleared {
		cleared[c] = true
	}
	for _, want := range []string{"p1/banned", "p1/scheduled_disable", "p2/banned", "p2/scheduled_disable"} {
		if !cleared[want] {
			t.Errorf("%s stays open after the account was reinstated (cleared %v)", want, notices.cleared)
		}
	}
	if len(notices.notified) != 1 || notices.notified[0][3:] != "reinstated" {
		t.Fatalf("one reinstated notice per account, notified %v", notices.notified)
	}
}

func TestAReactivatedNumberClosesItsSuspensionAlert(t *testing.T) {
	repo := newMockRepo()
	seedDialog360Phone(repo, "p1", "ch1", businessphone.StatusSuspended)
	notices := &recordingPhoneNotices{}
	reader := &fakeOwnerReader{suspended: []businessphone.OwnerPhone{{ID: "p1", Dialog360ChannelID: "ch1", Dialog360ClientID: "cl1"}}}
	uc := NewDeactivateExcessPhonesUseCase(&fakeEntitlements{total: 1}, reader, repo, &fakePartnerSvc{}).WithNotices(notices)

	if err := uc.OnEntitlementIncreased("ws", workspace_addon.EntitlementWhatsAppBusinessPhones); err != nil {
		t.Fatal(err)
	}
	if len(notices.cleared) != 1 || notices.cleared[0] != "p1/suspended" {
		t.Fatalf("cleared %v", notices.cleared)
	}
}
