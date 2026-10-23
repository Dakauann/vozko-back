package mercadopago

import (
	"strings"

	"vozko/domain/payment"
)

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

const (
	DetailAccredited             = "accredited"
	DetailPartiallyRefunded      = "partially_refunded"
	DetailExpired                = "expired"
	DetailPendingWaitingTransfer = "pending_waiting_transfer"
	DetailPendingWaitingPayment  = "pending_waiting_payment"
	DetailChargebackSettled      = "settled"
	DetailChargebackReimbursed   = "reimbursed"
)

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

func MapEvent(p *Payment) string {
	if p == nil {
		return ""
	}
	status := strings.ToLower(strings.TrimSpace(p.Status))
	detail := strings.ToLower(strings.TrimSpace(p.StatusDetail))

	switch status {
	case StatusApproved:
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
		return chargebackEvent(detail)

	case StatusInMediation:
		return payment.EventPaymentInMediation

	case StatusAuthorized:
		return payment.EventPaymentAuthorized

	case StatusCancelled:
		if detail == DetailExpired {
			return payment.EventPaymentOverdue
		}
		return payment.EventPaymentDeleted

	case StatusRejected:
		return payment.EventPaymentRejected

	case StatusInProcess:
		return payment.EventPaymentInAnalysis

	case StatusPending:
		return payment.EventPaymentCreated

	case "":
		return ""

	default:
		return unmappedStatusEvent(status)
	}
}

func chargebackEvent(detail string) string {
	switch detail {
	case DetailChargebackSettled:
		return payment.EventPaymentChargeback
	case DetailChargebackReimbursed:
		return payment.EventPaymentChargebackReversed
	default:
		return payment.EventPaymentChargebackRequested
	}
}

func unmappedStatusEvent(status string) string {
	return "MERCADOPAGO_STATUS_" + strings.ToUpper(status)
}

func ReversedValue(p *Payment) float64 {
	if p == nil {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(p.Status)) {
	case StatusChargedBack, StatusInMediation:
		return p.TransactionAmount
	default:
		return p.TransactionAmountRefunded
	}
}

func MapMethod(paymentMethodID string) payment.Method {
	switch strings.ToLower(strings.TrimSpace(paymentMethodID)) {
	case PaymentMethodPix:
		return payment.MethodPix
	case PaymentMethodBoleto, "boleto", "pec":
		return payment.MethodBoleto
	case "":
		return payment.MethodPix
	default:
		return payment.MethodCreditCard
	}
}

func PaymentMethodIDFor(m payment.Method) (string, error) {
	switch m {
	case payment.MethodPix, "":
		return PaymentMethodPix, nil
	case payment.MethodBoleto:
		return PaymentMethodBoleto, nil
	default:
		return "", payment.ErrMethodUnsupported
	}
}
