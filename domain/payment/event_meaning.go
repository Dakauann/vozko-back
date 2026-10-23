package payment

import (
	"errors"
	"strings"
)

var ErrUnmappedEvent = errors.New("payment event has no known meaning")

type EventMeaning string

const (
	MeaningSettled           EventMeaning = "settled"
	MeaningOverdue           EventMeaning = "overdue"
	MeaningCancelled         EventMeaning = "cancelled"
	MeaningReversalOpened    EventMeaning = "reversal_opened"
	MeaningReversalInDispute EventMeaning = "reversal_in_dispute"
	MeaningReversalFinal     EventMeaning = "reversal_final"
	MeaningReversalReleased  EventMeaning = "reversal_released"
	MeaningInformational     EventMeaning = "informational"
)

func (m EventMeaning) TakesMoneyBack() bool {
	return m == MeaningReversalOpened || m == MeaningReversalFinal
}

var eventMeanings = map[string]EventMeaning{
	EventPaymentCreated:                       MeaningInformational,
	"PAYMENT_AWAITING_RISK_ANALYSIS":          MeaningInformational,
	"PAYMENT_APPROVED_BY_RISK_ANALYSIS":       MeaningInformational,
	"PAYMENT_REPROVED_BY_RISK_ANALYSIS":       MeaningCancelled,
	EventPaymentAuthorized:                    MeaningInformational,
	"PAYMENT_UPDATED":                         MeaningInformational,
	EventPaymentConfirmed:                     MeaningInformational,
	EventPaymentReceived:                      MeaningSettled,
	EventPaymentReceivedInCash:                MeaningSettled,
	"PAYMENT_CREDIT_CARD_CAPTURE_REFUSED":     MeaningCancelled,
	"PAYMENT_ANTICIPATED":                     MeaningInformational,
	EventPaymentOverdue:                       MeaningOverdue,
	EventPaymentDeleted:                       MeaningCancelled,
	"PAYMENT_RESTORED":                        MeaningInformational,
	EventPaymentRefunded:                      MeaningReversalFinal,
	EventPaymentPartiallyRefunded:             MeaningReversalFinal,
	EventPaymentRefundInProgress:              MeaningReversalOpened,
	"PAYMENT_REFUND_DENIED":                   MeaningInformational,
	EventPaymentReceivedInCashUndone:          MeaningReversalFinal,
	EventPaymentChargebackRequested:           MeaningReversalOpened,
	EventPaymentChargebackDispute:             MeaningReversalInDispute,
	EventPaymentAwaitingChargebackReversal:    MeaningReversalInDispute,
	"PAYMENT_DUNNING_REQUESTED":               MeaningInformational,
	"PAYMENT_DUNNING_RECEIVED":                MeaningInformational,
	"PAYMENT_BANK_SLIP_CANCELLED":             MeaningInformational,
	"PAYMENT_BANK_SLIP_VIEWED":                MeaningInformational,
	"PAYMENT_CHECKOUT_VIEWED":                 MeaningInformational,
	"PAYMENT_SPLIT_CANCELLED":                 MeaningInformational,
	"PAYMENT_SPLIT_DIVERGENCE_BLOCK":          MeaningInformational,
	"PAYMENT_SPLIT_DIVERGENCE_BLOCK_FINISHED": MeaningInformational,
	"PAYMENT_SPLIT_DONE":                      MeaningInformational,
	EventPaymentInAnalysis:                    MeaningInformational,
	EventPaymentRejected:                      MeaningCancelled,
	EventPaymentChargeback:                    MeaningReversalFinal,
	EventPaymentInMediation:                   MeaningReversalOpened,
	EventPaymentChargebackReversed:            MeaningReversalReleased,
}

func MeaningOf(event string) (EventMeaning, bool) {
	meaning, ok := eventMeanings[strings.ToUpper(strings.TrimSpace(event))]
	return meaning, ok
}

type ReversalKind string

const (
	ReversalChargeback       ReversalKind = "chargeback"
	ReversalPixReturn        ReversalKind = "pix_return"
	ReversalUnexpectedRefund ReversalKind = "unexpected_refund"
)

func ReversalKindForBillingType(billingType string) ReversalKind {
	switch Method(strings.ToUpper(strings.TrimSpace(billingType))) {
	case MethodCreditCard:
		return ReversalChargeback
	case MethodPix:
		return ReversalPixReturn
	default:
		return ReversalUnexpectedRefund
	}
}
