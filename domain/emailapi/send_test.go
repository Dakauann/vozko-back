package emailapi

import (
	"testing"
	"time"
)

func TestOutstandingSkipsRecipientsAlreadySent(t *testing.T) {
	msgs := []Message{
		{Position: 0, Status: StatusDelivered, MessageID: "m0"},
		{Position: 1, Status: StatusPending},
		{Position: 2, Status: StatusRefused, ErrorCode: "email_send_throttled"},
		{Position: 3, Status: StatusQueued, MessageID: "m3"},
		{Position: 4, Status: StatusFailed, MessageID: "m4"},
	}
	got := Outstanding(msgs)
	if len(got) != 3 || got[0].Position != 1 || got[1].Position != 2 || got[2].Position != 4 {
		t.Fatalf("outstanding = %+v", got)
	}
}

func TestOpenRoundRefundsWhatDidNotGoOut(t *testing.T) {
	send := Send{Round: 2, RoundUnits: 3}
	msgs := []Message{
		{Position: 0, Round: 1, Status: StatusDelivered, MessageID: "a"},
		{Position: 1, Round: 2, Status: StatusSent, MessageID: "b"},
		{Position: 2, Round: 2, Status: StatusRefused},
		{Position: 3, Round: 2, Status: StatusPending},
	}
	if !send.RoundOpen() {
		t.Fatal("round 2 is open")
	}
	if got := send.UnitsToRefund(msgs); got != 2 {
		t.Fatalf("refund = %d, want 2", got)
	}
	settled := Send{Round: 2, RoundUnits: 3, RoundSettled: true}
	if settled.RoundOpen() || (Send{}).RoundOpen() {
		t.Fatal("a settled or absent round is not open")
	}
}

func TestRoundReferenceNamesTheSendAndRound(t *testing.T) {
	if got := RoundReference("s-1", 3); got != "email_api:s-1#3" {
		t.Fatalf("reference = %q", got)
	}
}

func TestLeaseHeld(t *testing.T) {
	now := time.Now()
	later := now.Add(time.Minute)
	earlier := now.Add(-time.Second)
	if (Send{}).LeaseHeld(now) || !(Send{LeaseUntil: &later}).LeaseHeld(now) || (Send{LeaseUntil: &earlier}).LeaseHeld(now) {
		t.Fatal("lease rule")
	}
}

func TestWentOutAndStatusValidity(t *testing.T) {
	for status, out := range map[MessageStatus]bool{
		StatusPending: false, StatusRefused: false, StatusQueued: true, StatusSent: true, StatusDelivered: true,
		StatusBounced: true, StatusSuppressed: true, StatusFailed: false,
	} {
		if (Message{Status: status, MessageID: "x"}).WentOut() != out {
			t.Fatalf("%s went out = %v", status, !out)
		}
	}
	if (Message{Status: StatusSent}).WentOut() {
		t.Fatal("a row without a message id never went out")
	}
	if !StatusSent.Valid() || MessageStatus("nope").Valid() {
		t.Fatal("status validity")
	}
}
