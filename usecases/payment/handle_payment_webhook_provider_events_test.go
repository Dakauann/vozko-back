package payment_usecase

import (
	"testing"

	"vozko/domain/invoice"
	"vozko/domain/payment"
)

func TestHandleWebhook_Rejected_CancelsInvoice(t *testing.T) {
	for _, purpose := range []invoice.Purpose{invoice.PurposeTopUp, invoice.PurposeSubscription, invoice.PurposeMonthlyBilling} {
		t.Run(string(purpose), func(t *testing.T) {
			repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
				"1": {ID: "inv-1", ExternalID: "1", Purpose: purpose, WorkspaceID: "ws-1", Status: invoice.StatusPending},
			}}
			uc := NewHandlePaymentWebhookUseCase(repo, nil, nil, nil, nil)

			if err := uc.Execute(&payment.WebhookEvent{
				Event:   payment.EventPaymentRejected,
				Payment: payment.WebhookPayment{ID: "1"},
			}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(repo.statusUpdates) != 1 || repo.statusUpdates[0] != invoice.StatusCancelled {
				t.Fatalf("expected CANCELLED, got %v", repo.statusUpdates)
			}
		})
	}
}

func TestHandleWebhook_InAnalysis_MovesNothing(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"1": {ID: "inv-1", ExternalID: "1", Purpose: invoice.PurposeTopUp, AmountUSD: 5000, WorkspaceID: "ws-1", Status: invoice.StatusPending},
	}}
	credit := &webhookCreditBalance{}
	debit := &webhookDebitBalance{}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, credit, debit, nil)

	if err := uc.Execute(&payment.WebhookEvent{
		Event:   payment.EventPaymentInAnalysis,
		Payment: payment.WebhookPayment{ID: "1"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.statusUpdates) != 0 {
		t.Fatalf("an in-analysis payment must not move invoice status, got %v", repo.statusUpdates)
	}
	if len(credit.calls) != 0 {
		t.Fatal("an in-analysis payment must never credit balance")
	}
	if len(debit.calls) != 0 {
		t.Fatal("an in-analysis payment must never debit balance")
	}
	if len(repo.markPaidCalls) != 0 {
		t.Fatal("an in-analysis payment must not be marked paid")
	}
}

func TestHandleWebhook_ProviderAgnostic(t *testing.T) {
	newRepo := func() *webhookInvoiceRepo {
		return &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
			"1234567890": {
				ID: "inv-1", ExternalID: "1234567890", Purpose: invoice.PurposeTopUp,
				AmountUSD: 5000, WorkspaceID: "ws-1", Status: invoice.StatusPending,
				AmountBRL: 30, ExchangeRate: 6,
			},
		}}
	}

	for _, provider := range []payment.Provider{payment.ProviderAsaas, payment.ProviderMercadoPago} {
		t.Run(string(provider), func(t *testing.T) {
			repo := newRepo()
			credit := &webhookCreditBalance{}
			uc := NewHandlePaymentWebhookUseCase(repo, nil, credit, nil, nil)

			if err := uc.Execute(&payment.WebhookEvent{
				Event:    payment.EventPaymentReceived,
				Provider: provider,
				Payment:  payment.WebhookPayment{ID: "1234567890", Value: 30},
			}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(credit.calls) != 1 {
				t.Fatalf("expected the top-up to be credited, got %d credits", len(credit.calls))
			}
			if credit.calls[0].Amount != 5000 {
				t.Fatalf("credited amount: got %d", credit.calls[0].Amount)
			}
		})
	}
}

func TestHandleWebhook_EventNamesAreNormalizedToUpper(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"1": {ID: "inv-1", ExternalID: "1", Purpose: invoice.PurposeTopUp, AmountUSD: 5000, WorkspaceID: "ws-1", Status: invoice.StatusPending, AmountBRL: 30, ExchangeRate: 6},
	}}
	credit := &webhookCreditBalance{}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, credit, nil, nil)

	if err := uc.Execute(&payment.WebhookEvent{
		Event:   "payment_received",
		Payment: payment.WebhookPayment{ID: "1"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(credit.calls) != 1 {
		t.Fatalf("a lowercase event name must still be handled, got %d credits", len(credit.calls))
	}
}
