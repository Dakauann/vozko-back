package whatsapp_campaign_entry

import "testing"

func TestChargeReferenceIdentifiesEachSendOfAnEntry(t *testing.T) {
	cases := map[int]string{0: "e-1", 1: "e-1:1", 2: "e-1:2", 10: "e-1:10"}
	for round, want := range cases {
		e := WhatsAppCampaignEntry{ID: "e-1", SendRound: round}
		if got := e.ChargeReference(); got != want {
			t.Errorf("round %d: reference = %q, want %q", round, got, want)
		}
	}
}
