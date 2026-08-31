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
		// Money under review or in dispute is not ours: it must not read as received.
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
			// The detail is not always set; the refunded amount alone must be enough,
			// or a partially refunded charge would keep re-crediting on every retry.
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
			name: "chargeback",
			p:    &Payment{Status: StatusChargedBack, TransactionAmount: 100},
			want: payment.EventPaymentChargeback,
		},
		{
			name: "authorized",
			p:    &Payment{Status: StatusAuthorized},
			want: payment.EventPaymentAuthorized,
		},
		{
			// An unpaid PIX that ran out its clock is "not paid in time", not "voided",
			// so the invoice stays recoverable rather than being cancelled.
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
			name: "in_mediation is informational only",
			p:    &Payment{Status: StatusInMediation},
			want: payment.EventPaymentInAnalysis,
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
			name: "unknown status moves nothing",
			p:    &Payment{Status: "status_from_the_future"},
			want: "",
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

// TestMapEvent_RefundedAmountWithoutTotal guards a division-free edge: a payment whose
// transaction_amount is zero must not be read as "fully refunded" and re-debited.
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
	// A card charge needs a client-side token this server never holds; silently
	// downgrading the customer to PIX would be worse than failing.
	if _, err := PaymentMethodIDFor(payment.MethodCreditCard); err == nil {
		t.Fatal("expected credit card to be rejected")
	}
}
