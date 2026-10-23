package carriermessaging_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/rcs"
)

func sentMessage(t *testing.T, h *harness, fallback carriermessaging.FallbackSpec) *carriermessaging.Message {
	t.Helper()
	req := marketing(rcs.Content{Text: "Oi"})
	req.Fallback = fallback
	msg, err := h.svc.Deliver(context.Background(), req)
	if err != nil {
		t.Fatalf("Deliver = %v", err)
	}
	return msg
}

func TestStatusReportsMoveTheMessageAndTheTranscript(t *testing.T) {
	h := newHarness(t)
	msg := sentMessage(t, h, carriermessaging.FallbackSpec{})

	for _, status := range []carriermessaging.Status{carriermessaging.StatusDelivered, carriermessaging.StatusRead} {
		if err := h.svc.ApplyStatus(context.Background(), carriermessaging.StatusReport{Reference: msg.Reference, Status: status, ReceivedAt: t0}); err != nil {
			t.Fatalf("ApplyStatus(%s) = %v", status, err)
		}
	}
	stored, _ := h.messages.FindByID(context.Background(), msg.ID)
	if stored.Status != carriermessaging.StatusRead {
		t.Fatalf("status = %s", stored.Status)
	}
	last := h.tstatus.updates[len(h.tstatus.updates)-1]
	if last.providerID != msg.Reference || last.status != conversation.DeliveryStatusRead {
		t.Fatalf("transcript = %+v", h.tstatus.updates)
	}
	if len(h.announcer.events) != len(h.tstatus.updates) {
		t.Fatal("every transcript change is announced to the inbox")
	}
}

func TestLateDeliveredReportDoesNothing(t *testing.T) {
	h := newHarness(t)
	msg := sentMessage(t, h, carriermessaging.FallbackSpec{})
	_ = h.svc.ApplyStatus(context.Background(), carriermessaging.StatusReport{Reference: msg.Reference, Status: carriermessaging.StatusRead, ReceivedAt: t0})
	updates := len(h.tstatus.updates)
	_ = h.svc.ApplyStatus(context.Background(), carriermessaging.StatusReport{Reference: msg.Reference, Status: carriermessaging.StatusDelivered, ReceivedAt: t0})
	if len(h.tstatus.updates) != updates {
		t.Fatal("a stale report must not touch the transcript")
	}
}

func TestFailedReportRefundsAndFallsBack(t *testing.T) {
	h := newHarness(t)
	msg := sentMessage(t, h, carriermessaging.FallbackSpec{SMSRouteID: "route-1", SMSText: "Oi"})

	err := h.svc.ApplyStatus(context.Background(), carriermessaging.StatusReport{Reference: msg.Reference, Status: carriermessaging.StatusFailed, ErrorCode: "3", ReceivedAt: t0})
	if err != nil {
		t.Fatalf("ApplyStatus = %v", err)
	}
	if len(h.ledger.credits) != 1 || len(h.provider.sms) != 1 {
		t.Fatalf("refunds = %d, sms = %d", len(h.ledger.credits), len(h.provider.sms))
	}
	_ = h.svc.ApplyStatus(context.Background(), carriermessaging.StatusReport{Reference: msg.Reference, Status: carriermessaging.StatusFailed, ErrorCode: "3", ReceivedAt: t0})
	if len(h.ledger.credits) != 1 || len(h.provider.sms) != 1 {
		t.Fatal("a repeated report must not refund or fall back twice")
	}
}

func TestUnreachableReportCountsAsFailed(t *testing.T) {
	h := newHarness(t)
	msg := sentMessage(t, h, carriermessaging.FallbackSpec{})
	_ = h.svc.ApplyStatus(context.Background(), carriermessaging.StatusReport{Reference: msg.Reference, Status: carriermessaging.StatusAccepted, Unreachable: true, ErrorCode: "42", ReceivedAt: t0})
	stored, _ := h.messages.FindByID(context.Background(), msg.ID)
	if stored.Status != carriermessaging.StatusFailed || stored.ErrorCode != "42" {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestUnknownReferenceIsReported(t *testing.T) {
	h := newHarness(t)
	err := h.svc.ApplyStatus(context.Background(), carriermessaging.StatusReport{Reference: "0123456789abcdef0123456789abcdef", Status: carriermessaging.StatusDelivered})
	if !errors.Is(err, carriermessaging.ErrMessageNotFound) {
		t.Fatalf("ApplyStatus = %v", err)
	}
}

func TestSMSStepStatusRefundsTheSMSCharge(t *testing.T) {
	h := newHarness(t)
	h.provider.outcome = carriermessaging.SendOutcome{ErrorCode: "1"}
	msg := sentMessage(t, h, carriermessaging.FallbackSpec{SMSRouteID: "route-1", SMSText: "Oi"})
	parent, _ := h.messages.FindByID(context.Background(), msg.ID)
	child, _ := h.messages.FindByID(context.Background(), parent.FallbackMessageID)

	_ = h.svc.ApplyStatus(context.Background(), carriermessaging.StatusReport{Reference: child.Reference, Status: carriermessaging.StatusFailed, ReceivedAt: t0})
	refunded := false
	for _, c := range h.ledger.credits {
		if *c.ReferenceID == "refund:"+carriermessaging.ChargeReference(carriermessaging.ChargeSMSSegment, child.ID) {
			refunded = true
		}
	}
	if !refunded {
		t.Fatalf("credits = %+v", h.ledger.credits)
	}
}

func TestExpirySweepExpiresAndFallsBack(t *testing.T) {
	h := newHarness(t)
	msg := sentMessage(t, h, carriermessaging.FallbackSpec{SMSRouteID: "route-1", SMSText: "Oi"})
	h.now = t0.Add(carriermessaging.FallbackValidity + time.Minute)

	n, err := h.svc.ExpireOverdue(context.Background(), 100)
	if err != nil || n != 1 {
		t.Fatalf("ExpireOverdue = %d, %v", n, err)
	}
	stored, _ := h.messages.FindByID(context.Background(), msg.ID)
	if stored.Status != carriermessaging.StatusExpired || len(h.provider.sms) != 1 {
		t.Fatalf("stored = %+v, sms = %d", stored, len(h.provider.sms))
	}
}
