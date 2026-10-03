package advertising

import (
	"testing"
	"time"
)

func settingsOn() ConversionSettings {
	return ConversionSettings{AdAccountID: "a", DatasetID: "ds-1", SendLeads: true, SendPurchases: true, Enabled: true}
}

func whatsappSignal(event DealEvent) DealSignal {
	return DealSignal{
		OpportunityID: "opp-1", Event: event, At: draftNow.Add(-time.Hour), ValueCents: 15_000, Currency: "brl",
		Identity: MessagingIdentity{Channel: ChannelWhatsApp, ClickID: "clid", WABAID: "waba"},
	}
}

func TestDealCreatedBecomesALeadAndWonBecomesAPurchaseWithValue(t *testing.T) {
	lead, skip := ConversionFor(settingsOn(), whatsappSignal(DealCreated), draftNow)
	if skip != "" || lead.Name != EventNameLead || lead.EventID != "opp-1:LeadSubmitted" || lead.ActionSource != "business_messaging" {
		t.Fatalf("lead %+v skip %s", lead, skip)
	}
	purchase, skip := ConversionFor(settingsOn(), whatsappSignal(DealWon), draftNow)
	if skip != "" || purchase.Name != EventNamePurchase || purchase.ValueMicros != 150_000_000 || purchase.Currency != "BRL" {
		t.Fatalf("purchase %+v skip %s", purchase, skip)
	}
}

func TestConversionsAreNeverSentWithoutWhatMetaNeeds(t *testing.T) {
	cases := map[SkipReason]func(*ConversionSettings, *DealSignal){
		SkipNotEnabled:   func(s *ConversionSettings, _ *DealSignal) { s.Enabled = false },
		SkipNoDataset:    func(s *ConversionSettings, _ *DealSignal) { s.DatasetID = "" },
		SkipEventOff:     func(s *ConversionSettings, _ *DealSignal) { s.SendLeads = false },
		SkipNoAdIdentity: func(_ *ConversionSettings, d *DealSignal) { d.Identity.ClickID = "" },
		SkipTooOld:       func(_ *ConversionSettings, d *DealSignal) { d.At = draftNow.Add(-8 * 24 * time.Hour) },
	}
	for want, mutate := range cases {
		s, d := settingsOn(), whatsappSignal(DealCreated)
		mutate(&s, &d)
		if event, got := ConversionFor(s, d, draftNow); event != nil || got != want {
			t.Fatalf("%s: event %+v reason %s", want, event, got)
		}
	}
	zero := whatsappSignal(DealWon)
	zero.ValueCents = 0
	if _, got := ConversionFor(settingsOn(), zero, draftNow); got != SkipValueMissing {
		t.Fatalf("purchase without value: %s", got)
	}
}

func TestMessengerAndInstagramNeedTheirScopedIDs(t *testing.T) {
	if (MessagingIdentity{Channel: ChannelMessenger, PageID: "p"}).Complete() {
		t.Fatal("messenger without psid")
	}
	if !(MessagingIdentity{Channel: ChannelInstagram, InstagramUserID: "ig", InstagramScoped: "igsid"}).Complete() {
		t.Fatal("instagram identity refused")
	}
}

func TestSettingsNeedADestinationAndSomethingToSend(t *testing.T) {
	s := ConversionSettings{AdAccountID: "a", Enabled: true}
	requireIssues(t, s.Validate(), FieldIssue{"datasetId", "required"}, FieldIssue{"sendLeads", "nothing_to_send"})
}

func TestFailedConversionsRetryAFewTimesOnly(t *testing.T) {
	if !(ConversionRecord{Status: ConversionFailed, Attempts: 1}).Retryable() || (ConversionRecord{Status: ConversionFailed, Attempts: 5}).Retryable() {
		t.Fatal("retry rule wrong")
	}
}

func TestDealsWithoutAnAdClickGoToThePixelWithHashedContact(t *testing.T) {
	s := settingsOn()
	s.DatasetID, s.PixelID = "", "px-1"
	signal := whatsappSignal(DealWon)
	signal.Identity = MessagingIdentity{}
	signal.Phone = "5511988887777"
	event, skip := ConversionFor(s, signal, draftNow)
	if skip != "" || event.Target != TargetPixel || event.TargetID != "px-1" || event.ActionSource != "system_generated" || event.PhoneHash != SHA256Hex("5511988887777") {
		t.Fatalf("event %+v skip %s", event, skip)
	}
	signal.Phone = ""
	if _, skip := ConversionFor(s, signal, draftNow); skip != SkipNoAdIdentity {
		t.Fatalf("no contact sent: %s", skip)
	}
}

func TestAdClicksPreferTheMessagingDataset(t *testing.T) {
	s := settingsOn()
	s.PixelID = "px-1"
	signal := whatsappSignal(DealCreated)
	signal.Phone = "5511988887777"
	if event, _ := ConversionFor(s, signal, draftNow); event.Target != TargetDataset || event.PhoneHash != "" {
		t.Fatalf("event %+v", event)
	}
}
