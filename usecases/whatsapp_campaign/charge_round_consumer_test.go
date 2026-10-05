package whatsapp_campaign_usecase

import (
	"errors"
	"testing"

	wce "vozko/domain/whatsapp_campaign_entry"
)

func roundHarness(t *testing.T, campID string) (*testHarness, string) {
	t.Helper()
	h := newTestHarness()
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}
	topic := setupCampaignWithCounter(h, campID, 10)
	_ = h.consumer.SubscribeToCampaign(campID)
	return h, topic
}

func TestConsumerChargesTheCurrentRound(t *testing.T) {
	h, topic := roundHarness(t, "camp-round")
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", SendRound: 2, Status: wce.SendStatusPending}

	h.queueSub.deliver(topic, makePayload("camp-round", "entry-1", "5584999990001"))

	if len(h.consumeTempl.executeRefs) != 1 || h.consumeTempl.executeRefs[0] != "entry-1:2" {
		t.Fatalf("charged %v, want the reference of round 2", h.consumeTempl.executeRefs)
	}
	if len(h.consumeTempl.refundRefs) != 0 {
		t.Fatalf("refunded %v, a delivered send is never refunded", h.consumeTempl.refundRefs)
	}
}

func TestConsumerRefundsAFailedSendUnderTheReferenceItCharged(t *testing.T) {
	h, topic := roundHarness(t, "camp-round-fail")
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", SendRound: 1, Status: wce.SendStatusPending}
	h.waClient.failSend = true

	h.queueSub.deliver(topic, makePayload("camp-round-fail", "entry-1", "5584999990001"))

	if len(h.consumeTempl.executeRefs) != 1 || len(h.consumeTempl.refundRefs) != 1 ||
		h.consumeTempl.executeRefs[0] != h.consumeTempl.refundRefs[0] || h.consumeTempl.refundRefs[0] != "entry-1:1" {
		t.Fatalf("charged %v, refunded %v: the refund must hit exactly the charge", h.consumeTempl.executeRefs, h.consumeTempl.refundRefs)
	}
}

func TestConsumerDropsAnEntryThatNoLongerExists(t *testing.T) {
	h, topic := roundHarness(t, "camp-gone")
	h.entryRepo.dispatched = false

	ack := h.queueSub.deliver(topic, makePayload("camp-gone", "entry-1", "5584999990001"))

	if ack == nil || !ack.acked.Load() || ack.nacked.Load() {
		t.Fatal("a deleted entry is dropped, not retried")
	}
	if h.consumeTempl.consumed.Load() != 0 || h.waClient.sendCalls.Load() != 0 {
		t.Fatal("nothing is charged or sent for an entry that no longer exists")
	}
}

func TestConsumerRequeuesWithoutChargingWhenTheEntryCannotBeRead(t *testing.T) {
	h, topic := roundHarness(t, "camp-unreadable")
	h.entryRepo.findErr = errors.New("db down")

	ack := h.queueSub.deliver(topic, makePayload("camp-unreadable", "entry-1", "5584999990001"))

	if ack == nil || !ack.nacked.Load() {
		t.Fatal("an unreadable entry is retried, never sent unbilled")
	}
	if h.consumeTempl.consumed.Load() != 0 || h.waClient.sendCalls.Load() != 0 {
		t.Fatal("without the round there is no reference to charge, so nothing goes out")
	}
}

func TestADeliveryReadsTheEntryOnce(t *testing.T) {
	h, topic := roundHarness(t, "camp-one-read")
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", Status: wce.SendStatusPending}

	h.queueSub.deliver(topic, makePayload("camp-one-read", "entry-1", "5584999990001"))

	if n := h.entryRepo.findByIDCount(); n != 1 {
		t.Fatalf("entry fetched %d times per delivery, want 1", n)
	}
}
