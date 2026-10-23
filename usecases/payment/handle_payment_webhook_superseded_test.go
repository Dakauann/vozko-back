package payment_usecase

import (
	"testing"

	"vozko/domain/invoice"
	"vozko/domain/payment"
)

type supersededRig struct {
	inv     *invoice.Invoice
	repo    *webhookInvoiceRepo
	credit  *webhookCreditBalance
	confirm *webhookConfirmMonthly
	book    *ledgerBook
	alerts  *fakeAlerts
	uc      *handlePaymentWebhookUseCase
}

func newSupersededRig(status invoice.Status) *supersededRig {
	inv := &invoice.Invoice{
		ID: "inv-m", WorkspaceID: "ws-1", Purpose: invoice.PurposeMonthlyBilling, Status: status,
		BillingType: "PIX", Provider: payment.ProviderAsaas, ExternalID: "pay_new",
		AmountBRL: 210, AmountUSD: 35_000_000, CreditableUSD: 35_000_000, ExchangeRate: 6,
	}
	rig := &supersededRig{
		inv: inv,
		repo: &webhookInvoiceRepo{
			byExternal:   map[string]*invoice.Invoice{"pay_new": inv},
			bySuperseded: map[string]*invoice.Invoice{"pay_old": inv},
			byID:         map[string]*invoice.Invoice{"inv-m": inv},
		},
		credit:  &webhookCreditBalance{},
		confirm: &webhookConfirmMonthly{},
		book:    newLedgerBook(),
		alerts:  &fakeAlerts{},
	}
	rig.uc = NewHandlePaymentWebhookUseCase(rig.repo, nil, rig.credit, bookDebit{rig.book}, nil).
		WithMonthlyBilling(rig.confirm).
		WithReversals(rig.book, &fakeLocks{}, rig.alerts)
	return rig
}

func (r *supersededRig) send(t *testing.T, event, chargeID string) {
	t.Helper()
	if err := r.uc.Execute(&payment.WebhookEvent{
		Event:    event,
		Provider: payment.ProviderAsaas,
		Payment:  payment.WebhookPayment{ID: chargeID, ExternalReference: invoice.ExternalReference("inv-m"), BillingType: "PIX"},
	}); err != nil {
		t.Fatalf("Execute(%s, %s): %v", event, chargeID, err)
	}
}

func TestTheCancellationOfASupersededChargeLeavesTheInvoiceAlone(t *testing.T) {
	for _, event := range []string{payment.EventPaymentDeleted, payment.EventPaymentOverdue, "PAYMENT_REPROVED_BY_RISK_ANALYSIS"} {
		rig := newSupersededRig(invoice.StatusOverdue)
		rig.send(t, event, "pay_old")

		if len(rig.repo.statusUpdates) != 0 || rig.inv.Status != invoice.StatusOverdue {
			t.Errorf("%s of the replaced charge must not move the invoice, got %v", event, rig.repo.statusUpdates)
		}
		if len(rig.repo.attached) != 0 {
			t.Errorf("%s of the replaced charge must never attach it again", event)
		}
	}
}

func TestAPaymentOnASupersededChargeSettlesTheInvoiceOnce(t *testing.T) {
	rig := newSupersededRig(invoice.StatusOverdue)

	rig.send(t, payment.EventPaymentReceived, "pay_old")
	rig.send(t, payment.EventPaymentReceived, "pay_old")

	if rig.inv.Status != invoice.StatusPaid {
		t.Fatalf("status = %s, want PAID: the money arrived for this invoice", rig.inv.Status)
	}
	if len(rig.credit.calls) != 1 || rig.credit.calls[0].Amount != 35_000_000 {
		t.Fatalf("the invoice must be credited exactly once, got %+v", rig.credit.calls)
	}
	if len(rig.confirm.calls) != 1 {
		t.Fatalf("the plan must be extended once, got %v", rig.confirm.calls)
	}
	if len(rig.alerts.subjects) == 0 {
		t.Fatal("a payment on a replaced charge must reach the operators so the current charge is cancelled")
	}
}

func TestPayingBothChargesNeverSettlesTwice(t *testing.T) {
	rig := newSupersededRig(invoice.StatusOverdue)

	rig.send(t, payment.EventPaymentReceived, "pay_new")
	rig.send(t, payment.EventPaymentReceived, "pay_old")

	if len(rig.credit.calls) != 1 {
		t.Fatalf("two payments for one invoice must credit once, got %d", len(rig.credit.calls))
	}
	if len(rig.confirm.calls) != 1 {
		t.Fatalf("two payments for one invoice must extend the plan once, got %v", rig.confirm.calls)
	}
	if len(rig.alerts.subjects) != 1 {
		t.Fatalf("the duplicate payment must reach the operators, got %v", rig.alerts.subjects)
	}
}

func TestAReversalOnASupersededChargeMovesNoBalance(t *testing.T) {
	rig := newSupersededRig(invoice.StatusPaid)
	rig.book.net[rig.book.key("ws-1", "inv-m")] = 35_000_000

	rig.send(t, payment.EventPaymentRefundInProgress, "pay_old")
	rig.send(t, payment.EventPaymentChargebackReversed, "pay_old")

	if len(rig.book.debits) != 0 || len(rig.book.credits) != 0 {
		t.Fatalf("a reversal of a replaced charge must not move the balance, got debits %v credits %v", rig.book.debits, rig.book.credits)
	}
	if rig.inv.Status != invoice.StatusPaid {
		t.Fatalf("status = %s, want PAID", rig.inv.Status)
	}
	if len(rig.alerts.subjects) != 2 {
		t.Fatalf("each reversal of a replaced charge must reach the operators, got %v", rig.alerts.subjects)
	}
}

func TestTheCurrentChargeStillSettlesAfterARenewal(t *testing.T) {
	rig := newSupersededRig(invoice.StatusOverdue)

	rig.send(t, payment.EventPaymentDeleted, "pay_old")
	rig.send(t, payment.EventPaymentReceived, "pay_new")

	if rig.inv.Status != invoice.StatusPaid || len(rig.credit.calls) != 1 {
		t.Fatalf("the renewed charge must settle the invoice, status %s credits %d", rig.inv.Status, len(rig.credit.calls))
	}
	if len(rig.alerts.subjects) != 0 {
		t.Fatalf("a normal payment raises no alert, got %v", rig.alerts.subjects)
	}
}

func TestAPaymentOnTheChargeBeingReplacedSettlesWithoutAnAlert(t *testing.T) {
	rig := newSupersededRig(invoice.StatusOverdue)
	rig.inv.ExternalID = "pay_old"
	rig.repo.byExternal = map[string]*invoice.Invoice{"pay_old": rig.inv}

	rig.send(t, payment.EventPaymentReceived, "pay_old")

	if rig.inv.Status != invoice.StatusPaid || len(rig.credit.calls) != 1 {
		t.Fatalf("a payment that beat the cancel settles the invoice, status %s credits %d", rig.inv.Status, len(rig.credit.calls))
	}
	if len(rig.alerts.subjects) != 0 {
		t.Fatalf("no other charge is open, so no alert, got %v", rig.alerts.subjects)
	}
}

func TestTheExpiryOfARenewedChargeNeverReopensAClosedInvoice(t *testing.T) {
	for _, status := range []invoice.Status{invoice.StatusExpired, invoice.StatusCancelled} {
		rig := newSupersededRig(status)
		rig.send(t, payment.EventPaymentOverdue, "pay_new")

		if rig.inv.Status != status || len(rig.repo.statusUpdates) != 0 {
			t.Errorf("an overdue event must not move a %s invoice back to OVERDUE, got %v", status, rig.repo.statusUpdates)
		}
	}
}
