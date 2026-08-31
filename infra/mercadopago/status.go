package mercadopago

import (
	"strings"

	"vozko/domain/payment"
)

// Payment statuses returned by /v1/payments.
//
// Reference: https://www.mercadopago.com/developers/en/docs/checkout-api/response-handling/collection-results
const (
	StatusPending     = "pending"
	StatusApproved    = "approved"
	StatusAuthorized  = "authorized"
	StatusInProcess   = "in_process"
	StatusInMediation = "in_mediation"
	StatusRejected    = "rejected"
	StatusCancelled   = "cancelled"
	StatusRefunded    = "refunded"
	StatusChargedBack = "charged_back"
)

// status_detail values this integration reasons about. Mercado Pago publishes many
// more (mostly card-rejection reasons) that need no special handling here.
const (
	DetailAccredited             = "accredited"
	DetailPartiallyRefunded      = "partially_refunded"
	DetailExpired                = "expired"
	DetailPendingWaitingTransfer = "pending_waiting_transfer"
	DetailPendingWaitingPayment  = "pending_waiting_payment"
)

// MapStatus translates a Mercado Pago payment status into the canonical
// payment.Status stored on our own payment rows.
//
// in_process and in_mediation map to Pending on purpose: the money is not ours yet,
// and treating a disputed payment as received would credit balance we may have to
// claw back.
func MapStatus(mpStatus string) payment.Status {
	switch strings.ToLower(strings.TrimSpace(mpStatus)) {
	case StatusApproved:
		return payment.StatusReceived
	case StatusRefunded:
		return payment.StatusRefunded
	case StatusChargedBack:
		return payment.StatusRefunded
	case StatusCancelled, StatusRejected:
		return payment.StatusCancelled
	default:
		return payment.StatusPending
	}
}

// MapEvent translates the state of a fetched Mercado Pago payment into the canonical
// webhook event vocabulary the shared handler switches on.
//
// This is a state-to-event translation rather than an event-to-event one, and that is
// forced by the provider: a Mercado Pago notification carries only an action label and
// a resource id, never the resulting state. The consumer therefore fetches the payment
// and derives the event from what the payment now *is*. That also makes the mapping
// naturally idempotent under redelivery.
//
// An empty return means "no local state should move" — the caller acknowledges the
// notification and does nothing.
func MapEvent(p *Payment) string {
	if p == nil {
		return ""
	}
	status := strings.ToLower(strings.TrimSpace(p.Status))
	detail := strings.ToLower(strings.TrimSpace(p.StatusDetail))

	switch status {
	case StatusApproved:
		// A partial refund leaves the payment approved and only moves status_detail,
		// so the refund check has to come before the "received" mapping or a partially
		// refunded charge would keep re-crediting.
		if detail == DetailPartiallyRefunded || (p.TransactionAmountRefunded > 0 && p.TransactionAmountRefunded < p.TransactionAmount) {
			return payment.EventPaymentPartiallyRefunded
		}
		if p.TransactionAmountRefunded > 0 && p.TransactionAmountRefunded >= p.TransactionAmount {
			return payment.EventPaymentRefunded
		}
		return payment.EventPaymentReceived

	case StatusRefunded:
		return payment.EventPaymentRefunded

	case StatusChargedBack:
		// Money already credited is being pulled back. Handled exactly like a refund
		// so the balance ledger stays honest.
		return payment.EventPaymentChargeback

	case StatusAuthorized:
		return payment.EventPaymentAuthorized

	case StatusCancelled:
		// An unpaid PIX or boleto that ran out its clock arrives as cancelled with
		// status_detail=expired. That is "not paid in time", not "voided by us", so it
		// maps to overdue and leaves the invoice recoverable rather than cancelled.
		if detail == DetailExpired {
			return payment.EventPaymentOverdue
		}
		return payment.EventPaymentDeleted

	case StatusRejected:
		return payment.EventPaymentRejected

	case StatusInProcess, StatusInMediation:
		// Under review or in dispute: informational only, deliberately moves nothing.
		return payment.EventPaymentInAnalysis

	case StatusPending:
		return payment.EventPaymentCreated

	default:
		return ""
	}
}

// MapMethod translates a Mercado Pago payment_method_id into the canonical method.
func MapMethod(paymentMethodID string) payment.Method {
	switch strings.ToLower(strings.TrimSpace(paymentMethodID)) {
	case PaymentMethodPix:
		return payment.MethodPix
	case PaymentMethodBoleto, "boleto", "pec":
		return payment.MethodBoleto
	case "":
		return payment.MethodPix
	default:
		// Every other id Mercado Pago issues in Brazil (visa, master, elo, ...) is a
		// card brand.
		return payment.MethodCreditCard
	}
}

// PaymentMethodIDFor spells a canonical method the way POST /v1/payments expects it.
func PaymentMethodIDFor(m payment.Method) (string, error) {
	switch m {
	case payment.MethodPix, "":
		return PaymentMethodPix, nil
	case payment.MethodBoleto:
		return PaymentMethodBoleto, nil
	default:
		// A card charge needs a tokenized card from the client SDK, which this
		// server-side integration never holds. Failing loudly beats silently
		// downgrading the customer to a different payment method.
		return "", payment.ErrMethodUnsupported
	}
}
