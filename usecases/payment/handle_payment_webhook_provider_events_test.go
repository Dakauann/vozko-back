package payment_usecase

import (
	"testing"

	"vozko/domain/balance"
	"vozko/domain/invoice"
	"vozko/domain/order"
	"vozko/domain/payment"
)

func TestHandleWebhook_Chargeback_DebitsLikeARefund(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"1234567890": {
			ID: "inv-1", ExternalID: "1234567890", Purpose: invoice.PurposeTopUp,
			AmountUSD: 5000, WorkspaceID: "ws-1", Status: invoice.StatusPaid,
			AmountBRL: 30, ExchangeRate: 6,
		},
	}}
	debit := &webhookDebitBalance{}
	uc := NewHandlePaymentWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, debit, nil)

	err := uc.Execute(&payment.WebhookEvent{
		Event:    payment.EventPaymentChargeback,
		Provider: payment.ProviderMercadoPago,
		Payment:  payment.WebhookPayment{ID: "1234567890"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(debit.calls) != 1 {
		t.Fatalf("expected the chargeback to debit the balance, got %d debits", len(debit.calls))
	}
	if !debit.calls[0].IsRefund {
		t.Fatal("a chargeback debit must be flagged as a refund")
	}
	if debit.calls[0].Amount != 5000 {
		t.Fatalf("expected the full credited amount to be reversed, got %d", debit.calls[0].Amount)
	}
	if len(repo.statusUpdates) != 1 || repo.statusUpdates[0] != invoice.StatusRefunded {
		t.Fatalf("expected the invoice to move to REFUNDED, got %v", repo.statusUpdates)
	}
}

func TestHandleWebhook_Chargeback_Subscription(t *testing.T) {
	planID := "plan-1"
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"1": {
			ID: "inv-1", ExternalID: "1", Purpose: invoice.PurposeSubscription,
			PlanDefinitionID: &planID, AmountUSD: 8000, WorkspaceID: "ws-1",
			Status: invoice.StatusPaid, AmountBRL: 48, ExchangeRate: 6,
		},
	}}
	debit := &webhookDebitBalance{}
	uc := NewHandlePaymentWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, debit, nil)

	if err := uc.Execute(&payment.WebhookEvent{
		Event:   payment.EventPaymentChargeback,
		Payment: payment.WebhookPayment{ID: "1"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(debit.calls) != 1 || debit.calls[0].ServiceType != balance.ServiceTopUp {
		t.Fatalf("expected one top-up debit, got %+v", debit.calls)
	}
}

func TestHandleWebhook_Chargeback_MonthlyBilling_ReversesOnlyCreditablePortion(t *testing.T) {
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"1": {
			ID: "inv-1", ExternalID: "1", Purpose: invoice.PurposeMonthlyBilling,
			AmountUSD: 10000, CreditableUSD: 4000, WorkspaceID: "ws-1",
			Status: invoice.StatusPaid, AmountBRL: 60, ExchangeRate: 6,
		},
	}}
	debit := &webhookDebitBalance{}
	uc := NewHandlePaymentWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, debit, nil)

	if err := uc.Execute(&payment.WebhookEvent{
		Event:   payment.EventPaymentChargeback,
		Payment: payment.WebhookPayment{ID: "1"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(debit.calls) != 1 {
		t.Fatalf("expected one debit, got %d", len(debit.calls))
	}
	if debit.calls[0].Amount != 4000 {
		t.Fatalf("expected only the creditable portion (4000) to be reversed, got %d", debit.calls[0].Amount)
	}
}

func TestHandleWebhook_Rejected_CancelsInvoice(t *testing.T) {
	for _, purpose := range []invoice.Purpose{invoice.PurposeTopUp, invoice.PurposeSubscription, invoice.PurposeMonthlyBilling} {
		t.Run(string(purpose), func(t *testing.T) {
			repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
				"1": {ID: "inv-1", ExternalID: "1", Purpose: purpose, WorkspaceID: "ws-1", Status: invoice.StatusPending},
			}}
			uc := NewHandlePaymentWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, nil, nil, nil)

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
	uc := NewHandlePaymentWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, credit, debit, nil)

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

func TestMapWebhookEventToStatuses_ProviderEvents(t *testing.T) {
	cases := []struct {
		event      string
		wantPay    *payment.Status
		wantOrder  *order.Status
		nilAllowed bool
	}{
		{
			event:     payment.EventPaymentChargeback,
			wantPay:   ptrPaymentStatus(payment.StatusRefunded),
			wantOrder: ptrOrderStatus(order.StatusRefunded),
		},
		{
			event:     payment.EventPaymentRejected,
			wantPay:   ptrPaymentStatus(payment.StatusCancelled),
			wantOrder: ptrOrderStatus(order.StatusCancelled),
		},
		{
			event:      payment.EventPaymentInAnalysis,
			nilAllowed: true,
		},
	}

	for _, c := range cases {
		t.Run(c.event, func(t *testing.T) {
			payStatus, ordStatus := mapWebhookEventToStatuses(c.event)
			if c.nilAllowed {
				if payStatus != nil || ordStatus != nil {
					t.Fatalf("%s must move nothing, got (%v,%v)", c.event, payStatus, ordStatus)
				}
				return
			}
			if payStatus == nil || *payStatus != *c.wantPay {
				t.Fatalf("payment status: got %v want %v", payStatus, *c.wantPay)
			}
			if ordStatus == nil || *ordStatus != *c.wantOrder {
				t.Fatalf("order status: got %v want %v", ordStatus, *c.wantOrder)
			}
		})
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
			uc := NewHandlePaymentWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, credit, nil, nil)

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
	uc := NewHandlePaymentWebhookUseCase(nil, nil, nil, nil, nil, repo, nil, credit, nil, nil)

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

func ptrPaymentStatus(s payment.Status) *payment.Status { return &s }
func ptrOrderStatus(s order.Status) *order.Status       { return &s }
