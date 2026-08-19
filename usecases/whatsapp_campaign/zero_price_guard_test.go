package whatsapp_campaign_usecase

import (
	"testing"
	"time"

	"vozko/domain/balance"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

// An unpriced workspace used to send at BULK volume for free.
//
// The pricer answers a missing item with a zero price and no error, and the
// consumer only checked the error. It reserved nothing, "debited" nothing —
// Execute returned (nil, nil), which reads as success — and sent anyway. Meta
// bills per delivered message, so the platform paid for up to a whole campaign
// and collected nothing, with no log line and no alert.
//
// The guard now lives in the billing use case, so all four senders inherit it;
// this pins the consumer's half: refuse, fail the entry, and do not spin.
func TestConsumer_ZeroPrice_RefusesToSendUnbilled(t *testing.T) {
	h := newTestHarness()
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}
	h.consumeTempl.costErr = balance.ErrPriceUnavailable
	h.cachedBalanceChecker.balanceMicros = 1_000_000

	campID := "camp-zero-price"
	topic := makeTopic(campID)
	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning, WorkspaceID: "ws-1",
		BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", LeadID: "lead-1"}
	if err := h.consumer.SubscribeToCampaign(campID); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	h.queueSub.deliver(topic, makePayload(campID, "entry-1", "5584999990001"))
	time.Sleep(50 * time.Millisecond)

	if got := h.waClient.sendCalls.Load(); got != 0 {
		t.Fatalf("sent %d message(s) with no price configured; every one of them is free for us and billed by Meta", got)
	}
	if status := h.entryRepo.entries["entry-1"].Status; status != wce.SendStatusFailed {
		t.Fatalf("entry status = %q, want FAILED so the refusal is visible rather than silent", status)
	}
}

// The refusal is a configuration fault, not a transient one. Requeuing it would
// spin the entry against the broker forever.
func TestConsumer_ZeroPrice_DoesNotRequeueForever(t *testing.T) {
	h := newTestHarness()
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}
	h.consumeTempl.costErr = balance.ErrPriceUnavailable

	campID := "camp-zero-price-requeue"
	topic := makeTopic(campID)
	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning, WorkspaceID: "ws-1",
		BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", LeadID: "lead-1"}
	_ = h.consumer.SubscribeToCampaign(campID)

	h.queueSub.deliver(topic, makePayload(campID, "entry-1", "5584999990001"))
	time.Sleep(50 * time.Millisecond)

	h.queuePub.mu.Lock()
	republished := len(h.queuePub.messages[topic]) + len(h.queuePub.delayedMessages[topic])
	h.queuePub.mu.Unlock()
	if republished > 0 {
		t.Fatalf("republished %d time(s); an unpriced workspace will still be unpriced on the retry", republished)
	}
}

// Defence in depth: even if the pricer stops erroring and simply answers zero,
// the consumer must not send. A campaign is up to 150k messages, and every one
// of them would be billed by Meta and charged to nobody.
func TestConsumer_ZeroCostWithoutError_StillRefusesToSend(t *testing.T) {
	h := newTestHarness()
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}
	h.consumeTempl.zeroCost = true // returns (0, nil), the old fail-open shape

	campID := "camp-zero-cost-no-error"
	topic := makeTopic(campID)
	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning, WorkspaceID: "ws-1",
		BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", LeadID: "lead-1"}
	_ = h.consumer.SubscribeToCampaign(campID)

	h.queueSub.deliver(topic, makePayload(campID, "entry-1", "5584999990001"))
	time.Sleep(50 * time.Millisecond)

	if got := h.waClient.sendCalls.Load(); got != 0 {
		t.Fatalf("sent %d message(s) at zero price", got)
	}
}
