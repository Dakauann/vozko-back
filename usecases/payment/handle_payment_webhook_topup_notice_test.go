package payment_usecase

import (
	"strings"
	"testing"

	"vozko/domain/invoice"
	"vozko/domain/notification"
	"vozko/domain/payment"
	"vozko/domain/workspace"
	"vozko/usecases/notification/notificationtest"
)

func paidTopUp(t *testing.T, publisher *notificationtest.Publisher) {
	t.Helper()
	repo := &webhookInvoiceRepo{byExternal: map[string]*invoice.Invoice{
		"1": {ID: "inv-1", ExternalID: "1", Purpose: invoice.PurposeTopUp, AmountUSD: 12_500_000, WorkspaceID: "ws-1", Status: invoice.StatusPending, AmountBRL: 75, ExchangeRate: 6},
	}}
	uc := NewHandlePaymentWebhookUseCase(repo, nil, &webhookCreditBalance{}, &webhookDebitBalance{}, nil).
		WithNotifications(publisher, "https://app.vozko.test/")
	for i := 0; i < 2; i++ {
		if err := uc.Execute(&payment.WebhookEvent{Event: payment.EventPaymentReceived, Payment: payment.WebhookPayment{ID: "1"}}); err != nil {
			t.Fatalf("execute: %v", err)
		}
	}
}

func TestAPaidTopUpTellsTheBillingTeamTheBalanceArrived(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	paidTopUp(t, publisher)

	d := publisher.Only(t)
	if d.WorkspaceID != "ws-1" || d.Category != notification.CategoryBilling || d.Severity != notification.SeveritySuccess || !d.Email {
		t.Fatalf("draft = %+v", d)
	}
	if !d.Audience.Equal(notification.Holding(workspace.ResourceBalance, workspace.ActionRead)) {
		t.Fatalf("audience = %+v, want the people who see the balance, not only the owner", d.Audience)
	}
	if !strings.Contains(d.Body, "R$ 75,00") || d.ActionURL != "/dashboard/balance" || d.DedupKey != "wallet_topup:inv-1" {
		t.Fatalf("body %q link %q dedup %q", d.Body, d.ActionURL, d.DedupKey)
	}
	if d.EmailCopy == nil || d.EmailCopy.Template != "wallet_topup_confirmed.html" || d.EmailCopy.Placeholders["Amount"] != "R$ 75,00" ||
		d.EmailCopy.Placeholders["DashboardURL"] != "https://app.vozko.test/dashboard/balance" {
		t.Fatalf("email = %+v", d.EmailCopy)
	}
}
