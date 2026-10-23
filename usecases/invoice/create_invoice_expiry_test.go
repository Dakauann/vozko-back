package invoice_usecase

import (
	"testing"
	"time"

	billing "vozko/domain/billing"
	"vozko/domain/invoice"
)

func TestTheMonthlyChargeStaysPayableUntilTheCutoff(t *testing.T) {
	repo := &stubInvoiceRepo{}
	gw := newStubGateway()
	due := time.Date(2026, time.October, 23, 0, 0, 0, 0, billing.LocationBRT())

	out, err := newCreditableUC(repo, gw).Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", Purpose: invoice.PurposeMonthlyBilling,
		AmountBRL: 210, CreditableBRL: 210, BillingType: "PIX", DueDate: &due,
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	cutoff, _ := invoice.PayableUntil(invoice.PurposeMonthlyBilling, &due)
	if !gw.lastRequest.DueDate.Equal(due) {
		t.Fatalf("the charge keeps the invoice due date, got %s", gw.lastRequest.DueDate)
	}
	if !gw.lastRequest.ExpiresAt.Equal(cutoff) {
		t.Fatalf("the PIX must stay payable until the cutoff %s, got %s", cutoff, gw.lastRequest.ExpiresAt)
	}
	if out.Invoice.ChargeExpiresAt == nil || !out.Invoice.ChargeExpiresAt.Equal(cutoff) {
		t.Fatalf("without a provider expiry the invoice stores the requested one, got %v", out.Invoice.ChargeExpiresAt)
	}
}

func TestATopUpChargeHasNoRenewableWindow(t *testing.T) {
	repo := &stubInvoiceRepo{}
	gw := newStubGateway()

	if _, err := newCreditableUC(repo, gw).Execute(invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1", Purpose: invoice.PurposeTopUp, AmountBRL: 50, BillingType: "PIX",
	}); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !gw.lastRequest.ExpiresAt.IsZero() {
		t.Fatalf("a top-up expires with its due date, got an explicit expiry %s", gw.lastRequest.ExpiresAt)
	}
	if gw.lastRequest.DueDate.Sub(time.Now()) > invoice.DefaultChargeLifetime {
		t.Fatalf("a top-up is due within the default charge lifetime, got %s", gw.lastRequest.DueDate)
	}
}
