package payment_usecase

import (
	"errors"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/invoice"
	"vozko/domain/payment"
)

type erroringCreditBalance struct{ err error }

func (u *erroringCreditBalance) Execute(balance.CreditBalanceInput) (*balance.Transaction, error) {
	return nil, u.err
}

type webhookConfirmMonthly struct {
	calls []string
	err   error
}

func (c *webhookConfirmMonthly) Execute(workspaceID string) error {
	c.calls = append(c.calls, workspaceID)
	return c.err
}

func TestHandleAsaasWebhook_MonthlyBillingCreditsOnlyPlanPortionAndExtends(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-m": {
			ID:            "inv-m",
			ExternalID:    "pay-m",
			WorkspaceID:   "ws-1",
			Purpose:       invoice.PurposeMonthlyBilling,
			AmountBRL:     1399,
			AmountUSD:     233_166_667, // plan + channels, in USD
			CreditableUSD: 183_166_667, // plan portion only
			ExchangeRate:  6,
		},
	}}
	credit := &webhookCreditBalance{}
	confirm := &webhookConfirmMonthly{}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, credit, &webhookDebitBalance{}, nil).
		WithMonthlyBilling(confirm)

	if err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-m"}}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(credit.calls) != 1 {
		t.Fatalf("expected one balance credit, got %d", len(credit.calls))
	}
	if credit.calls[0].Amount != 183_166_667 {
		t.Fatalf("credit Amount = %d, want 183_166_667 (plan portion only, NOT the full %d)", credit.calls[0].Amount, 233_166_667)
	}
	if credit.calls[0].WorkspaceID != "ws-1" {
		t.Fatalf("credit workspace = %q, want ws-1", credit.calls[0].WorkspaceID)
	}
	if len(confirm.calls) != 1 || confirm.calls[0] != "ws-1" {
		t.Fatalf("expected subscription extension for ws-1, got %v", confirm.calls)
	}
}

func TestHandleAsaasWebhook_MonthlyBillingCreditableFallsBackToAmount(t *testing.T) {
	// An invoice created before CreditableUSD existed (CreditableUSD == 0) credits the full AmountUSD.
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-old": {
			ID: "inv-old", ExternalID: "pay-old", WorkspaceID: "ws-9",
			Purpose: invoice.PurposeMonthlyBilling, AmountBRL: 60, AmountUSD: 10_000_000, CreditableUSD: 0, ExchangeRate: 6,
		},
	}}
	credit := &webhookCreditBalance{}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, credit, &webhookDebitBalance{}, nil)

	if err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-old"}}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(credit.calls) != 1 || credit.calls[0].Amount != 10_000_000 {
		t.Fatalf("expected fallback credit of the full AmountUSD, got %+v", credit.calls)
	}
}

func TestHandleAsaasWebhook_MonthlyBillingAlreadyPaidDoesNotReprocess(t *testing.T) {
	paid := &invoice.Invoice{
		ID: "inv-p", ExternalID: "pay-p", WorkspaceID: "ws-1",
		Purpose: invoice.PurposeMonthlyBilling, AmountUSD: 10_000_000, CreditableUSD: 10_000_000,
		Status: invoice.StatusPaid, ExchangeRate: 6,
	}
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{"pay-p": paid}}
	credit := &webhookCreditBalance{}
	confirm := &webhookConfirmMonthly{}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, credit, &webhookDebitBalance{}, nil).
		WithMonthlyBilling(confirm)

	if err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-p"}}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(credit.calls) != 0 || len(confirm.calls) != 0 {
		t.Fatalf("an already-paid invoice must not re-credit or re-extend, credit=%d confirm=%d", len(credit.calls), len(confirm.calls))
	}
}

func TestHandleAsaasWebhook_MonthlyBillingOverdueMarksOverdue(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-o": {ID: "inv-o", ExternalID: "pay-o", WorkspaceID: "ws-1", Purpose: invoice.PurposeMonthlyBilling, AmountUSD: 10_000_000, ExchangeRate: 6},
	}}
	credit := &webhookCreditBalance{}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, credit, &webhookDebitBalance{}, nil)

	if err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_OVERDUE", Payment: payment.AsaasWebhookPayment{ID: "pay-o"}}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(repo.statusUpdates) != 1 || repo.statusUpdates[0] != invoice.StatusOverdue {
		t.Fatalf("an overdue monthly invoice must be marked Overdue (drives the dunning window), got %v", repo.statusUpdates)
	}
	if len(credit.calls) != 0 {
		t.Fatalf("an overdue event must never credit saldo, got %d", len(credit.calls))
	}
}

func TestHandleAsaasWebhook_MonthlyBillingRefundDebitsWhenPaid(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-r": {ID: "inv-r", ExternalID: "pay-r", WorkspaceID: "ws-1", Purpose: invoice.PurposeMonthlyBilling,
			AmountBRL: 1099, AmountUSD: 10_000_000, CreditableUSD: 8_000_000, Status: invoice.StatusPaid, ExchangeRate: 6},
	}}
	debit := &webhookDebitBalance{}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, &webhookCreditBalance{}, debit, nil)

	if err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_REFUNDED", Payment: payment.AsaasWebhookPayment{ID: "pay-r"}}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// The refund claws back exactly the plan portion that was credited (not the channel repasse).
	if len(debit.calls) != 1 || debit.calls[0].Amount != 8_000_000 {
		t.Fatalf("expected one debit of the creditable plan portion (8_000_000), got %+v", debit.calls)
	}
	if !debit.calls[0].IsRefund {
		t.Fatal("a refund debit must be marked IsRefund")
	}
	if last := repo.statusUpdates[len(repo.statusUpdates)-1]; last != invoice.StatusRefunded {
		t.Fatalf("expected status Refunded, got %v", repo.statusUpdates)
	}
}

func TestHandleAsaasWebhook_MonthlyBillingRefundSkipsDebitWhenNeverPaid(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-rn": {ID: "inv-rn", ExternalID: "pay-rn", WorkspaceID: "ws-1", Purpose: invoice.PurposeMonthlyBilling,
			AmountUSD: 10_000_000, CreditableUSD: 8_000_000, Status: invoice.StatusPending, ExchangeRate: 6},
	}}
	debit := &webhookDebitBalance{}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, &webhookCreditBalance{}, debit, nil)

	if err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_PARTIALLY_REFUNDED", Payment: payment.AsaasWebhookPayment{ID: "pay-rn"}}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Nothing was ever credited (invoice was never paid), so nothing may be clawed back.
	if len(debit.calls) != 0 {
		t.Fatalf("a never-paid invoice must not be debited on refund, got %+v", debit.calls)
	}
	if last := repo.statusUpdates[len(repo.statusUpdates)-1]; last != invoice.StatusRefunded {
		t.Fatalf("expected status Refunded, got %v", repo.statusUpdates)
	}
}

func TestHandleAsaasWebhook_MonthlyBillingCreditFailureRollsBackToPending(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"pay-f": {ID: "inv-f", ExternalID: "pay-f", WorkspaceID: "ws-1", Purpose: invoice.PurposeMonthlyBilling,
			AmountUSD: 10_000_000, CreditableUSD: 10_000_000, ExchangeRate: 6},
	}}
	credit := &erroringCreditBalance{err: errors.New("ledger down")}
	confirm := &webhookConfirmMonthly{}
	uc := NewHandleAsaasWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, credit, &webhookDebitBalance{}, nil).
		WithMonthlyBilling(confirm)

	err := uc.Execute(&payment.AsaasWebhookEvent{Event: "PAYMENT_RECEIVED", Payment: payment.AsaasWebhookPayment{ID: "pay-f"}})
	if err == nil {
		t.Fatal("a saldo-credit failure must surface as an error so the webhook is retried")
	}
	// Rolled back to Pending so a retry re-credits from a clean state, and subscriptions were NOT extended.
	if last := repo.statusUpdates[len(repo.statusUpdates)-1]; last != invoice.StatusPending {
		t.Fatalf("expected rollback to Pending after a failed credit, got %v", repo.statusUpdates)
	}
	if len(confirm.calls) != 0 {
		t.Fatalf("subscriptions must not be extended when the credit failed, got %v", confirm.calls)
	}
}
