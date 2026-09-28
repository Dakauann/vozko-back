package whatsapp_campaign_usecase

import (
	"fmt"
	"testing"

	"vozko/domain/balance"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

func TestMonthlyCap_ReachedDuringDrain_FailsEntryWithCapCodeAndCompletes(t *testing.T) {
	h := newTestHarness()
	campID := "camp-monthly-cap"
	topic := setupCampaignWithCounter(h, campID, 1)

	h.consumeTempl.executeErr = fmt.Errorf("debit: %w", balance.ErrMonthlySendCapReached)

	ack := h.queueSub.deliver(topic, makePayload(campID, "entry-1", "5584999990001"))
	if ack == nil || !ack.acked.Load() || ack.nacked.Load() {
		t.Fatal("a capped entry is a permanent failure: acked, never nacked")
	}
	if status := h.entryRepo.getStatus("entry-1"); status != wce.SendStatusFailed {
		t.Fatalf("expected FAILED, got %s", status)
	}
	if code := h.entryRepo.getErrorCode("entry-1"); code != wce.ErrorCodeMonthlySendCapReached {
		t.Fatalf("expected error code %d, got %d", wce.ErrorCodeMonthlySendCapReached, code)
	}
	if msg := h.entryRepo.getErrorMessage("entry-1"); msg != balance.ErrMonthlySendCapReached.Error() {
		t.Fatalf("unexpected error message %q", msg)
	}
	if delayed := h.queuePub.delayedMessagesFor(topic); len(delayed) != 0 {
		t.Fatalf("a capped entry must not loop in retry, got %d delayed", len(delayed))
	}
	if h.consumeTempl.consumed.Load() != 0 || h.consumeTempl.refunded.Load() != 0 {
		t.Fatal("nothing was charged, so nothing is refunded")
	}
	inflight, _ := h.inflightReserver.GetInflight("ws-1")
	if inflight != 0 {
		t.Fatalf("the inflight reservation is released, got %d", inflight)
	}
	camp, _ := h.campaignRepo.FindByID(campID)
	if camp.Status != wc.CampaignStatusCompleted {
		t.Fatalf("the campaign completes once every entry is settled, got %s", camp.Status)
	}
}
