package whatsapp_campaign_usecase

import (
	"errors"
	"strings"
	"testing"
	"time"

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

type fakeChargeClock struct {
	at    time.Time
	found bool
	err   error
}

func (f fakeChargeClock) ChargedAt(string) (time.Time, bool, error) { return f.at, f.found, f.err }

var settleNow = time.Date(2026, 10, 23, 18, 0, 0, 0, time.UTC)

func chargedEarlier(t *testing.T, campID string, clock fakeChargeClock) (*testHarness, *mockAck, string) {
	t.Helper()
	h, topic := roundHarness(t, campID)
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", SendRound: 1, Status: wce.SendStatusPending}
	h.consumeTempl.alreadyCharged = true
	h.consumer.SetChargeClock(clock)
	h.consumer.now = func() time.Time { return settleNow }
	ack := h.queueSub.deliver(topic, makePayload(campID, "entry-1", "5584999990001"))
	return h, ack, topic
}

func TestADeliveryThatDidNotCreateTheChargeWaitsWhileTheOtherMayStillBeSending(t *testing.T) {
	h, ack, topic := chargedEarlier(t, "camp-raced", fakeChargeClock{at: settleNow.Add(-30 * time.Second), found: true})

	if ack == nil || !ack.acked.Load() || ack.nacked.Load() {
		t.Fatal("the losing delivery leaves the queue and comes back later")
	}
	if h.waClient.sendCalls.Load() != 0 || len(h.consumeTempl.refundRefs) != 0 || h.entryRepo.entries["entry-1"].Status != wce.SendStatusPending {
		t.Fatalf("sent %d, refunded %v, status %s; the winning delivery may still be sending",
			h.waClient.sendCalls.Load(), h.consumeTempl.refundRefs, h.entryRepo.entries["entry-1"].Status)
	}
	if later := h.queuePub.delayedMessagesFor(topic); len(later) != 1 || later[0].delay != 90*time.Second {
		t.Fatalf("rechecks %+v, want one recheck when the grace runs out", later)
	}
}

func TestAChargedSendThatNeverFinishedIsRefundedAndFailedSoTheWaveCanClose(t *testing.T) {
	h, ack, _ := chargedEarlier(t, "camp-interrupted", fakeChargeClock{at: settleNow.Add(-20 * time.Minute), found: true})

	if ack == nil || !ack.acked.Load() {
		t.Fatal("a settled interrupted send leaves the queue")
	}
	if h.waClient.sendCalls.Load() != 0 {
		t.Fatal("an interrupted send is never sent again under the same charge")
	}
	if len(h.consumeTempl.refundRefs) != 1 || h.consumeTempl.refundRefs[0] != "entry-1:1" {
		t.Fatalf("refunded %v, want exactly the interrupted charge", h.consumeTempl.refundRefs)
	}
	if e := h.entryRepo.entries["entry-1"]; e.Status != wce.SendStatusFailed || e.ErrorCode != wce.ErrorCodeSendInterrupted {
		t.Fatalf("entry = %s/%d, want failed as interrupted", e.Status, e.ErrorCode)
	}
}

func TestAnInterruptedSendWhoseChargeCannotBeReadIsNeitherRefundedNorFailed(t *testing.T) {
	for name, clock := range map[string]fakeChargeClock{
		"read error":  {err: errors.New("db down")},
		"charge gone": {found: false},
	} {
		h, _, _ := chargedEarlier(t, "camp-unreadable-"+strings.ReplaceAll(name, " ", "-"), clock)
		if len(h.consumeTempl.refundRefs) != 0 || h.entryRepo.entries["entry-1"].Status != wce.SendStatusPending || h.waClient.sendCalls.Load() != 0 {
			t.Fatalf("%s: refunded %v, status %s, sent %d; nothing is settled on a guess",
				name, h.consumeTempl.refundRefs, h.entryRepo.entries["entry-1"].Status, h.waClient.sendCalls.Load())
		}
	}
}

func TestAnInterruptedSendWhoseRefundFailsStaysPendingForTheNextCheck(t *testing.T) {
	h, topic := roundHarness(t, "camp-refund-fails")
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", SendRound: 1, Status: wce.SendStatusPending}
	h.consumeTempl.alreadyCharged = true
	h.consumeTempl.refundErr = errors.New("ledger busy")
	h.consumer.SetChargeClock(fakeChargeClock{at: settleNow.Add(-20 * time.Minute), found: true})
	h.consumer.now = func() time.Time { return settleNow }

	h.queueSub.deliver(topic, makePayload("camp-refund-fails", "entry-1", "5584999990001"))

	if h.entryRepo.entries["entry-1"].Status != wce.SendStatusPending || len(h.queuePub.delayedMessagesFor(topic)) != 1 {
		t.Fatal("a failed refund leaves the entry pending and checks again, so the customer is never left charged for a failure")
	}
}

func TestWithoutAChargeClockTheConsumerChecksAgainInsteadOfGuessing(t *testing.T) {
	h, topic := roundHarness(t, "camp-no-clock")
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", SendRound: 1, Status: wce.SendStatusPending}
	h.consumeTempl.alreadyCharged = true

	h.queueSub.deliver(topic, makePayload("camp-no-clock", "entry-1", "5584999990001"))

	if len(h.consumeTempl.refundRefs) != 0 || h.entryRepo.entries["entry-1"].Status != wce.SendStatusPending || len(h.queuePub.delayedMessagesFor(topic)) != 1 {
		t.Fatal("without the clock nothing is refunded or failed")
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
