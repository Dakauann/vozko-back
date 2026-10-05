package conversation_usecase

import (
	"testing"

	"vozko/domain/conversation"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

func refundHarness() (*handleWhatsAppMessageUseCase, *settlementBilling, *mockWhatsAppSharedState) {
	billing := &settlementBilling{}
	state := newMockWhatsAppSharedState()
	uc := &handleWhatsAppMessageUseCase{
		consumeWhatsappTemplate: billing,
		wcCampaignRepo: routingCampaigns{byID: map[string]*wc.Campaign{
			"campaign-1": {ID: "campaign-1", WorkspaceID: "ws-1"},
		}},
		sharedState: state,
	}
	return uc, billing, state
}

func failedStatus(messageID string) conversation.WhatsAppStatus {
	return conversation.WhatsAppStatus{
		ID:      messageID,
		Pricing: &conversation.WhatsAppPricing{Category: "utility"},
	}
}

func campaignEntry(id string) *wce.WhatsAppCampaignEntry {
	return &wce.WhatsAppCampaignEntry{ID: id, CampaignID: "campaign-1"}
}

func TestEveryFailedEntryOfACampaignIsRefundedUnderItsOwnReference(t *testing.T) {
	uc, billing, _ := refundHarness()

	if err := uc.refundFailedWhatsAppCampaignEntry(failedStatus("wamid.1"), campaignEntry("entry-1")); err != nil {
		t.Fatal(err)
	}
	if err := uc.refundFailedWhatsAppCampaignEntry(failedStatus("wamid.2"), campaignEntry("entry-2")); err != nil {
		t.Fatal(err)
	}

	if len(billing.refunds) != 2 || billing.refunds[0] != "entry-1" || billing.refunds[1] != "entry-2" {
		t.Fatalf("refunds = %v, each failure must be refunded under the entry it charged", billing.refunds)
	}
}

func TestARedeliveredFailureIsRefundedOnce(t *testing.T) {
	uc, billing, _ := refundHarness()

	for i := 0; i < 2; i++ {
		if err := uc.refundFailedWhatsAppCampaignEntry(failedStatus("wamid.1"), campaignEntry("entry-1")); err != nil {
			t.Fatal(err)
		}
	}

	if len(billing.refunds) != 1 {
		t.Fatalf("refunds = %v, a redelivered webhook must not refund twice", billing.refunds)
	}
}

func TestARedeliveryAfterTheGuardExpiredReusesTheChargeReference(t *testing.T) {
	uc, billing, state := refundHarness()

	if err := uc.refundFailedWhatsAppCampaignEntry(failedStatus("wamid.1"), campaignEntry("entry-1")); err != nil {
		t.Fatal(err)
	}
	state.store = map[string]bool{}
	if err := uc.refundFailedWhatsAppCampaignEntry(failedStatus("wamid.1"), campaignEntry("entry-1")); err != nil {
		t.Fatal(err)
	}

	if len(billing.refunds) != 2 || billing.refunds[0] != billing.refunds[1] || billing.refunds[0] != "entry-1" {
		t.Fatalf("refunds = %v, both attempts must carry the entry reference so the ledger refunds it once", billing.refunds)
	}
}

func TestAFailureAfterAResetIsRefundedUnderItsRound(t *testing.T) {
	uc, billing, _ := refundHarness()
	entry := campaignEntry("entry-1")
	entry.SendRound = 1

	if err := uc.refundFailedWhatsAppCampaignEntry(failedStatus("wamid.2"), entry); err != nil {
		t.Fatal(err)
	}

	if len(billing.refunds) != 1 || billing.refunds[0] != "entry-1:1" {
		t.Fatalf("refunds = %v, the resend charged entry-1:1 and must be refunded under it", billing.refunds)
	}
}
