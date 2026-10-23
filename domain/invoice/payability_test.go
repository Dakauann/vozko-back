package invoice

import (
	"errors"
	"testing"
	"time"

	billing "vozko/domain/billing"
)

func brt(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, billing.LocationBRT())
}

func text(value string) *string { return &value }

func at(value time.Time) *time.Time { return &value }

func monthlyInvoice(status Status) *Invoice {
	return &Invoice{
		ID:          "inv-1",
		Purpose:     PurposeMonthlyBilling,
		Status:      status,
		BillingType: "PIX",
		ExternalID:  "pay_1",
		PixQrCode:   text("qr-image"),
		PixCopy:     text("000201pix"),
		DueDate:     at(brt(2026, time.October, 23, 0, 0)),
		CreatedAt:   brt(2026, time.October, 18, 9, 0),
	}
}

func TestTheMonthlyInvoiceIsPayableUntilTheCutoff(t *testing.T) {
	until, ok := PayableUntil(PurposeMonthlyBilling, at(brt(2026, time.October, 23, 0, 0)))
	if !ok {
		t.Fatal("a monthly invoice with a due date must have a payable window")
	}
	want := brt(2026, time.October, 27, 23, 59).Add(59 * time.Second)
	if !until.Equal(want) {
		t.Fatalf("PayableUntil = %s, want %s", until, want)
	}
}

func TestOnlyMonthlyInvoicesHaveARenewableWindow(t *testing.T) {
	for _, purpose := range []Purpose{PurposeTopUp, PurposeSubscription, ""} {
		if _, ok := PayableUntil(purpose, at(brt(2026, time.October, 23, 0, 0))); ok {
			t.Errorf("%q must not have a renewable window", purpose)
		}
	}
	if _, ok := PayableUntil(PurposeMonthlyBilling, nil); ok {
		t.Error("a monthly invoice without a due date has no window to renew in")
	}
}

func TestAPendingInvoiceWithALiveChargeShowsItsCode(t *testing.T) {
	inv := monthlyInvoice(StatusPending)
	inv.ChargeExpiresAt = at(brt(2026, time.October, 27, 23, 59))

	view := inv.PaymentAt(brt(2026, time.October, 20, 10, 0))

	if view.State != PaymentOpen {
		t.Fatalf("state = %s, want OPEN", view.State)
	}
	if view.PixCopy == nil || *view.PixCopy != "000201pix" || view.PixQrCode == nil {
		t.Fatal("an open payment must carry the copy and paste code and the QR image")
	}
	if view.ExpiresAt == nil || !view.ExpiresAt.Equal(*inv.ChargeExpiresAt) {
		t.Fatalf("expiresAt = %v, want the charge expiry", view.ExpiresAt)
	}
	if view.PayableUntil == nil || view.CanRenew() {
		t.Fatal("an open monthly payment knows its window and needs no renewal")
	}
}

func TestAnExpiredChargeOnAPayableInvoiceIsRenewable(t *testing.T) {
	inv := monthlyInvoice(StatusPending)
	inv.ChargeExpiresAt = at(brt(2026, time.October, 23, 0, 0))

	view := inv.PaymentAt(brt(2026, time.October, 23, 10, 0))

	if view.State != PaymentRenewable || !view.CanRenew() {
		t.Fatalf("state = %s, want RENEWABLE", view.State)
	}
	if view.PixCopy != nil || view.PixQrCode != nil {
		t.Fatal("an expired code must never be shown")
	}
}

func TestAChargeAboutToExpireIsNotOffered(t *testing.T) {
	inv := monthlyInvoice(StatusPending)
	now := brt(2026, time.October, 22, 10, 0)
	inv.ChargeExpiresAt = at(now.Add(ChargeExpiryMargin / 2))

	if view := inv.PaymentAt(now); view.State != PaymentRenewable {
		t.Fatalf("a code that dies before the customer can pay must be renewed, got %s", view.State)
	}
}

func TestAnOverdueInvoiceBeforeTheCutoffIsRenewable(t *testing.T) {
	inv := monthlyInvoice(StatusOverdue)

	view := inv.PaymentAt(brt(2026, time.October, 25, 9, 0))

	if view.State != PaymentRenewable {
		t.Fatalf("an overdue invoice with no known live charge must be renewable, got %s", view.State)
	}
	if view.PayableUntil == nil || view.PayableUntil.Day() != 27 {
		t.Fatalf("payableUntil = %v, want the cutoff on the 27th", view.PayableUntil)
	}
}

func TestAnOverdueInvoiceWithARenewedChargeShowsTheNewCode(t *testing.T) {
	inv := monthlyInvoice(StatusOverdue)
	inv.ChargeExpiresAt = at(brt(2026, time.October, 27, 23, 59))

	if view := inv.PaymentAt(brt(2026, time.October, 25, 9, 0)); view.State != PaymentOpen {
		t.Fatalf("a renewed charge on an overdue invoice must be payable, got %s", view.State)
	}
}

func TestNothingIsPayableAfterTheCutoff(t *testing.T) {
	inv := monthlyInvoice(StatusOverdue)
	inv.ChargeExpiresAt = at(brt(2026, time.November, 30, 0, 0))

	view := inv.PaymentAt(brt(2026, time.October, 28, 0, 0))

	if view.State != PaymentClosed || view.PixCopy != nil {
		t.Fatalf("after the cutoff the invoice is closed, got %s", view.State)
	}
}

func TestSettledAndClosedInvoicesNeverExposeACode(t *testing.T) {
	for _, status := range []Status{StatusPaid, StatusRefunded, StatusReversed, StatusCancelled, StatusExpired} {
		inv := monthlyInvoice(status)
		inv.ChargeExpiresAt = at(brt(2026, time.October, 27, 23, 59))
		view := inv.PaymentAt(brt(2026, time.October, 20, 10, 0))
		if view.State != PaymentClosed || view.PixCopy != nil || view.PixQrCode != nil || view.CanRenew() {
			t.Errorf("%s must be closed with no code, got %+v", status, view)
		}
	}
}

func TestAReservationWithoutAChargeIsNotPayable(t *testing.T) {
	inv := monthlyInvoice(StatusPending)
	inv.ExternalID = ""

	if view := inv.PaymentAt(brt(2026, time.October, 20, 10, 0)); view.State != PaymentClosed {
		t.Fatalf("a reservation has nothing to pay yet, got %s", view.State)
	}
}

func TestOnlyPixInvoicesHaveAPixPayment(t *testing.T) {
	inv := monthlyInvoice(StatusPending)
	inv.BillingType = "BOLETO"
	inv.ChargeExpiresAt = at(brt(2026, time.October, 27, 23, 59))

	if view := inv.PaymentAt(brt(2026, time.October, 20, 10, 0)); view.State != PaymentClosed {
		t.Fatalf("a boleto invoice has no PIX payment, got %s", view.State)
	}
}

func TestATopUpIsPayableWhileItsChargeLivesButIsNeverRenewed(t *testing.T) {
	created := brt(2026, time.October, 20, 10, 0)
	inv := &Invoice{
		ID: "inv-2", Purpose: PurposeTopUp, Status: StatusPending, BillingType: "PIX",
		ExternalID: "pay_2", PixCopy: text("000201pix"), CreatedAt: created,
	}

	if view := inv.PaymentAt(created.Add(time.Hour)); view.State != PaymentOpen || view.PayableUntil != nil {
		t.Fatalf("a fresh top-up must be payable without a renewal window, got %+v", view)
	}
	if view := inv.PaymentAt(created.Add(DefaultChargeLifetime + time.Hour)); view.State != PaymentClosed {
		t.Fatalf("an expired top-up is closed so the customer quotes a new one, got %s", view.State)
	}
}

func TestALegacyChargeWithoutAnExpiryLivesUntilTheDueDateOnlyWhilePending(t *testing.T) {
	inv := monthlyInvoice(StatusPending)

	if view := inv.PaymentAt(brt(2026, time.October, 22, 10, 0)); view.State != PaymentOpen {
		t.Fatalf("a pending legacy charge before its due date is payable, got %s", view.State)
	}
	if view := inv.PaymentAt(brt(2026, time.October, 23, 10, 0)); view.State != PaymentRenewable {
		t.Fatalf("a legacy charge past its due date must be renewed, got %s", view.State)
	}
}

func TestTheExposedExpiryNeverOutlivesThePayableWindow(t *testing.T) {
	inv := monthlyInvoice(StatusPending)
	inv.ChargeExpiresAt = at(brt(2027, time.October, 23, 0, 0))

	view := inv.PaymentAt(brt(2026, time.October, 20, 10, 0))

	if view.ExpiresAt == nil || !view.ExpiresAt.Equal(*view.PayableUntil) {
		t.Fatalf("expiresAt = %v, want it capped at the window %v", view.ExpiresAt, view.PayableUntil)
	}
}

func TestARenewalRunsUntilTheEndOfTheWindow(t *testing.T) {
	inv := monthlyInvoice(StatusOverdue)

	deadline, err := inv.RenewalDeadline(brt(2026, time.October, 25, 9, 0))
	if err != nil {
		t.Fatalf("RenewalDeadline: %v", err)
	}
	until, _ := PayableUntil(inv.Purpose, inv.DueDate)
	if !deadline.Equal(until) {
		t.Fatalf("deadline = %s, want %s", deadline, until)
	}
}

func TestOnlyARenewableInvoiceHasARenewalDeadline(t *testing.T) {
	paid := monthlyInvoice(StatusPaid)
	if _, err := paid.RenewalDeadline(brt(2026, time.October, 25, 9, 0)); !errors.Is(err, ErrNotPayable) {
		t.Fatalf("a paid invoice must refuse a renewal, got %v", err)
	}
	live := monthlyInvoice(StatusPending)
	live.ChargeExpiresAt = at(brt(2026, time.October, 27, 23, 59))
	if _, err := live.RenewalDeadline(brt(2026, time.October, 20, 9, 0)); !errors.Is(err, ErrChargeStillLive) {
		t.Fatalf("a live charge needs no renewal, got %v", err)
	}
}

func TestOnlyPendingAndOverdueInvoicesAwaitPayment(t *testing.T) {
	for status, want := range map[Status]bool{
		StatusPending: true, StatusOverdue: true, StatusPaid: false, StatusCancelled: false,
		StatusExpired: false, StatusRefunded: false, StatusReversed: false,
	} {
		inv := monthlyInvoice(status)
		if got := inv.AwaitingPayment(); got != want {
			t.Errorf("%s AwaitingPayment = %v, want %v", status, got, want)
		}
	}
}

func TestEachRenewalHasItsOwnStableProviderKey(t *testing.T) {
	first := RenewalIdempotencyKey("inv-1", "pay_1")
	if first != RenewalIdempotencyKey("inv-1", "pay_1") {
		t.Fatal("a retried renewal of the same charge must reuse its key")
	}
	if first == RenewalIdempotencyKey("inv-1", "pay_2") || first == ExternalReference("inv-1") {
		t.Fatal("a renewal must never reuse the key of the charge it replaces")
	}
}
