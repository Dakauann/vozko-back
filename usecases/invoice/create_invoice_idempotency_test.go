package invoice_usecase

import (
	"testing"

	"vozko/domain/invoice"
)

func TestCreateInvoice_IdempotentReturnsExistingWithoutSecondCharge(t *testing.T) {
	repo := &stubInvoiceRepo{}
	asaasSvc := &stubAsaasService{}
	uc := newCreditableUC(repo, asaasSvc)

	key := "monthly:ws-1:2026-03"
	in := invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1",
		Purpose: invoice.PurposeMonthlyBilling, AmountBRL: 1399, CreditableBRL: 1099,
		BillingType: "PIX", IdempotencyKey: key,
	}

	first, err := uc.Execute(in)
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if first.Invoice.IdempotencyKey != key {
		t.Fatalf("first invoice should carry the idempotency key, got %q", first.Invoice.IdempotencyKey)
	}
	if asaasSvc.createCalls != 1 {
		t.Fatalf("first emit should charge Asaas exactly once, got %d", asaasSvc.createCalls)
	}

	// Re-run with the same key (an emit retry): must return the existing invoice and NOT charge again.
	second, err := uc.Execute(in)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if asaasSvc.createCalls != 1 {
		t.Fatalf("re-emit with the same key must not charge Asaas again, got %d calls", asaasSvc.createCalls)
	}
	if second.Invoice.ID != first.Invoice.ID {
		t.Fatalf("re-emit should return the same invoice, got %q want %q", second.Invoice.ID, first.Invoice.ID)
	}
}

func TestCreateInvoice_EmptyKeySkipsIdempotencyLookup(t *testing.T) {
	repo := &stubInvoiceRepo{}
	asaasSvc := &stubAsaasService{}
	uc := newCreditableUC(repo, asaasSvc)

	in := invoice.CreateInvoiceInput{
		WorkspaceID: "ws-1", UserID: "user-1",
		Purpose: invoice.PurposeMonthlyBilling, AmountBRL: 100, CreditableBRL: 100, BillingType: "PIX",
	}
	if _, err := uc.Execute(in); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if _, err := uc.Execute(in); err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if asaasSvc.createCalls != 2 {
		t.Fatalf("without an idempotency key each call charges, got %d", asaasSvc.createCalls)
	}
}
