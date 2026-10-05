package whatsapp_campaign_usecase

import (
	"testing"

	wce "vozko/domain/whatsapp_campaign_entry"
)

func TestConsumerNeverResendsAnEntryThisRoundAlreadyHandled(t *testing.T) {
	for _, status := range []wce.SendStatus{
		wce.SendStatusSent, wce.SendStatusDelivered, wce.SendStatusRead,
		wce.SendStatusFailed, wce.SendStatusNotEligiblePossibleSpam,
	} {
		h, topic := roundHarness(t, "camp-handled")
		h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", Status: status}

		ack := h.queueSub.deliver(topic, makePayload("camp-handled", "entry-1", "5584999990001"))

		if ack == nil || !ack.acked.Load() || ack.nacked.Load() {
			t.Fatalf("%s: a redelivery of a handled entry is dropped, not retried", status)
		}
		if len(h.consumeTempl.executeRefs) != 0 || h.waClient.sendCalls.Load() != 0 || len(h.consumeTempl.refundRefs) != 0 {
			t.Fatalf("%s: charged %v, sent %d, refunded %v; a handled entry is left alone",
				status, h.consumeTempl.executeRefs, h.waClient.sendCalls.Load(), h.consumeTempl.refundRefs)
		}
	}
}

func TestConsumerSendsOnlyTheDeliveryThatCreatedTheCharge(t *testing.T) {
	h, topic := roundHarness(t, "camp-raced")
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", SendRound: 1, Status: wce.SendStatusPending}
	h.consumeTempl.alreadyCharged = true

	ack := h.queueSub.deliver(topic, makePayload("camp-raced", "entry-1", "5584999990001"))

	if ack == nil || !ack.acked.Load() || ack.nacked.Load() {
		t.Fatal("the losing delivery is dropped, not retried")
	}
	if h.waClient.sendCalls.Load() != 0 {
		t.Fatal("a delivery that did not create the charge must not send, or the message goes out unpaid")
	}
	if len(h.consumeTempl.refundRefs) != 0 {
		t.Fatalf("refunded %v; the winning delivery's charge is not this delivery's to refund", h.consumeTempl.refundRefs)
	}
}

func TestADuplicatedQueueMessageSendsAndChargesOnce(t *testing.T) {
	h, topic := roundHarness(t, "camp-duplicated")
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", Status: wce.SendStatusPending}
	payload := makePayload("camp-duplicated", "entry-1", "5584999990001")

	h.queueSub.deliver(topic, payload)
	h.queueSub.deliver(topic, payload)

	if h.waClient.sendCalls.Load() != 1 || len(h.consumeTempl.executeRefs) != 1 {
		t.Fatalf("sent %d, charged %v; a reset of a paused campaign leaves two queue messages per entry and only one may go out",
			h.waClient.sendCalls.Load(), h.consumeTempl.executeRefs)
	}
}

func TestAFailedSendIsNotRetriedForFreeByARedelivery(t *testing.T) {
	h, topic := roundHarness(t, "camp-failed-redelivery")
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", Status: wce.SendStatusPending}
	h.waClient.failSend = true
	payload := makePayload("camp-failed-redelivery", "entry-1", "5584999990001")

	h.queueSub.deliver(topic, payload)
	h.waClient.failSend = false
	h.queueSub.deliver(topic, payload)

	if h.waClient.sendCalls.Load() != 1 || len(h.consumeTempl.executeRefs) != 1 || len(h.consumeTempl.refundRefs) != 1 {
		t.Fatalf("sent %d, charged %v, refunded %v; the failure is refunded once and only a reset may send it again",
			h.waClient.sendCalls.Load(), h.consumeTempl.executeRefs, h.consumeTempl.refundRefs)
	}
}
