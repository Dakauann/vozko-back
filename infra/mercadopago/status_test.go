package mercadopago

import (
	"testing"

	"vozko/domain/payment"
)

func TestMapStatus(t *testing.T) {
	cases := []struct {
		mp   string
		want payment.Status
	}{
		{StatusApproved, payment.StatusReceived},
		{StatusRefunded, payment.StatusRefunded},
		{StatusChargedBack, payment.StatusRefunded},
		{StatusCancelled, payment.StatusCancelled},
		{StatusRejected, payment.StatusCancelled},
		{StatusPending, payment.StatusPending},
		{StatusAuthorized, payment.StatusPending},
		{StatusInProcess, payment.StatusPending},
		{StatusInMediation, payment.StatusPending},
		{"", payment.StatusPending},
		{"something_new", payment.StatusPending},
		{"  APPROVED  ", payment.StatusReceived},
	}
	for _, c := range cases {
		if got := MapStatus(c.mp); got != c.want {
			t.Errorf("MapStatus(%q)=%q want %q", c.mp, got, c.want)
		}
	}
}

func TestMapEvent(t *testing.T) {
	cases := []struct {
		name string
		p    *Payment
		want string
	}{
		{
			name: "approved and accredited is received",
			p:    &Payment{Status: StatusApproved, StatusDetail: DetailAccredited, TransactionAmount: 100},
			want: payment.EventPaymentReceived,
		},
		{
			name: "approved with partially_refunded detail",
			p:    &Payment{Status: StatusApproved, StatusDetail: DetailPartiallyRefunded, TransactionAmount: 100, TransactionAmountRefunded: 30},
			want: payment.EventPaymentPartiallyRefunded,
		},
		{
			name: "approved with partial refund amount but no detail",
			p:    &Payment{Status: StatusApproved, TransactionAmount: 100, TransactionAmountRefunded: 30},
			want: payment.EventPaymentPartiallyRefunded,
		},
		{
			name: "approved but fully refunded by amount",
			p:    &Payment{Status: StatusApproved, TransactionAmount: 100, TransactionAmountRefunded: 100},
			want: payment.EventPaymentRefunded,
		},
		{
			name: "explicit refunded status",
			p:    &Payment{Status: StatusRefunded, TransactionAmount: 100},
			want: payment.EventPaymentRefunded,
		},
		{
			name: "chargeback still being decided is an opened reversal",
			p:    &Payment{Status: StatusChargedBack, StatusDetail: "in_process", TransactionAmount: 100},
			want: payment.EventPaymentChargebackRequested,
		},
		{
			name: "chargeback lost by the seller is final",
			p:    &Payment{Status: StatusChargedBack, StatusDetail: "settled", TransactionAmount: 100},
			want: payment.EventPaymentChargeback,
		},
		{
			name: "chargeback won by the seller releases the money",
			p:    &Payment{Status: StatusChargedBack, StatusDetail: "reimbursed", TransactionAmount: 100},
			want: payment.EventPaymentChargebackReversed,
		},
		{
			name: "chargeback with an unknown detail is treated as opened",
			p:    &Payment{Status: StatusChargedBack, StatusDetail: "detail_from_the_future", TransactionAmount: 100},
			want: payment.EventPaymentChargebackRequested,
		},
		{
			name: "authorized",
			p:    &Payment{Status: StatusAuthorized},
			want: payment.EventPaymentAuthorized,
		},
		{
			name: "cancelled because expired maps to overdue",
			p:    &Payment{Status: StatusCancelled, StatusDetail: DetailExpired},
			want: payment.EventPaymentOverdue,
		},
		{
			name: "cancelled by collector",
			p:    &Payment{Status: StatusCancelled, StatusDetail: "by_collector"},
			want: payment.EventPaymentDeleted,
		},
		{
			name: "rejected",
			p:    &Payment{Status: StatusRejected, StatusDetail: "cc_rejected_high_risk"},
			want: payment.EventPaymentRejected,
		},
		{
			name: "in_process is informational only",
			p:    &Payment{Status: StatusInProcess},
			want: payment.EventPaymentInAnalysis,
		},
		{
			name: "in_mediation holds the money, so it opens a reversal",
			p:    &Payment{Status: StatusInMediation},
			want: payment.EventPaymentInMediation,
		},
		{
			name: "pending",
			p:    &Payment{Status: StatusPending, StatusDetail: DetailPendingWaitingTransfer},
			want: payment.EventPaymentCreated,
		},
		{
			name: "case and whitespace insensitive",
			p:    &Payment{Status: "  APPROVED ", StatusDetail: " ACCREDITED ", TransactionAmount: 10},
			want: payment.EventPaymentReceived,
		},
		{
			name: "unknown status surfaces as an unmapped event instead of being dropped",
			p:    &Payment{Status: "status_from_the_future"},
			want: "MERCADOPAGO_STATUS_STATUS_FROM_THE_FUTURE",
		},
		{
			name: "empty status moves nothing",
			p:    &Payment{},
			want: "",
		},
		{
			name: "nil payment moves nothing",
			p:    nil,
			want: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MapEvent(c.p); got != c.want {
				t.Fatalf("MapEvent()=%q want %q", got, c.want)
			}
		})
	}
}

func TestEveryMappedEventHasAMeaning(t *testing.T) {
	for _, status := range []string{StatusPending, StatusApproved, StatusAuthorized, StatusInProcess, StatusInMediation, StatusRejected, StatusCancelled, StatusRefunded, StatusChargedBack} {
		for _, detail := range []string{"", DetailAccredited, DetailPartiallyRefunded, DetailExpired, "in_process", "settled", "reimbursed"} {
			event := MapEvent(&Payment{Status: status, StatusDetail: detail, TransactionAmount: 100})
			if _, ok := payment.MeaningOf(event); !ok {
				t.Errorf("status=%s detail=%s maps to %q, which has no meaning", status, detail, event)
			}
		}
	}
}

func TestTheWebhookEventCarriesTheReversedValue(t *testing.T) {
	cases := []struct {
		name string
		p    *Payment
		want float64
	}{
		{"partial refund carries the refunded amount", &Payment{ID: 1, Status: StatusApproved, TransactionAmount: 100, TransactionAmountRefunded: 30}, 30},
		{"full refund carries the refunded amount", &Payment{ID: 1, Status: StatusRefunded, TransactionAmount: 100, TransactionAmountRefunded: 100}, 100},
		{"chargeback reverses the whole amount", &Payment{ID: 1, Status: StatusChargedBack, StatusDetail: "in_process", TransactionAmount: 100}, 100},
		{"mediation holds the whole amount", &Payment{ID: 1, Status: StatusInMediation, TransactionAmount: 100}, 100},
		{"a received payment reverses nothing", &Payment{ID: 1, Status: StatusApproved, StatusDetail: DetailAccredited, TransactionAmount: 100}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event, ok := ToWebhookEvent("n-1", tc.p)
			if !ok {
				t.Fatal("expected an event")
			}
			if event.Payment.ReversedValue != tc.want {
				t.Fatalf("ReversedValue = %v, want %v", event.Payment.ReversedValue, tc.want)
			}
		})
	}
}

func TestMapEvent_RefundedAmountWithoutTotal(t *testing.T) {
	got := MapEvent(&Payment{Status: StatusApproved, TransactionAmount: 0, TransactionAmountRefunded: 0})
	if got != payment.EventPaymentReceived {
		t.Fatalf("got %q, want %q", got, payment.EventPaymentReceived)
	}
}

func TestMapMethod(t *testing.T) {
	cases := []struct {
		id   string
		want payment.Method
	}{
		{PaymentMethodPix, payment.MethodPix},
		{PaymentMethodBoleto, payment.MethodBoleto},
		{"boleto", payment.MethodBoleto},
		{"pec", payment.MethodBoleto},
		{"", payment.MethodPix},
		{"visa", payment.MethodCreditCard},
		{"master", payment.MethodCreditCard},
		{"elo", payment.MethodCreditCard},
		{"  PIX  ", payment.MethodPix},
	}
	for _, c := range cases {
		if got := MapMethod(c.id); got != c.want {
			t.Errorf("MapMethod(%q)=%q want %q", c.id, got, c.want)
		}
	}
}

func TestPaymentMethodIDFor(t *testing.T) {
	if got, err := PaymentMethodIDFor(payment.MethodPix); err != nil || got != PaymentMethodPix {
		t.Fatalf("PIX: got (%q,%v)", got, err)
	}
	if got, err := PaymentMethodIDFor(""); err != nil || got != PaymentMethodPix {
		t.Fatalf("empty method should default to PIX: got (%q,%v)", got, err)
	}
	if got, err := PaymentMethodIDFor(payment.MethodBoleto); err != nil || got != PaymentMethodBoleto {
		t.Fatalf("boleto: got (%q,%v)", got, err)
	}
	if _, err := PaymentMethodIDFor(payment.MethodCreditCard); err == nil {
		t.Fatal("expected credit card to be rejected")
	}
}
