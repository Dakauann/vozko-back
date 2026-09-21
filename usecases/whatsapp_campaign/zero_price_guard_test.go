package whatsapp_campaign_usecase

import (
	"testing"
	"time"

	"vozko/domain/balance"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

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

func TestConsumer_ZeroCostWithoutError_StillRefusesToSend(t *testing.T) {
	h := newTestHarness()
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}
	h.consumeTempl.zeroCost = true

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
