package invoice

import (
	"errors"
	"strings"
	"time"

	billing "vozko/domain/billing"
	"vozko/domain/payment"
)

var (
	ErrNotPayable         = errors.New("invoice is not payable")
	ErrAlreadyPaid        = errors.New("the payment for this invoice was already received")
	ErrChargeStillLive    = errors.New("the current PIX charge is still payable")
	ErrPixUnavailable     = errors.New("a new PIX charge could not be issued")
	ErrPixRenewInProgress = errors.New("a new PIX charge is already being issued")
)

const (
	DefaultChargeLifetime = 72 * time.Hour
	ChargeExpiryMargin    = 2 * time.Minute
	ChargeRenewalLease    = 3 * time.Minute
)

type PaymentState string

const (
	PaymentClosed    PaymentState = "CLOSED"
	PaymentOpen      PaymentState = "OPEN"
	PaymentRenewable PaymentState = "RENEWABLE"
)

type PaymentView struct {
	State        PaymentState
	PixQrCode    *string
	PixCopy      *string
	ExpiresAt    *time.Time
	PayableUntil *time.Time
}

func (v PaymentView) CanRenew() bool { return v.State == PaymentRenewable }

func PayableUntil(purpose Purpose, dueDate *time.Time) (time.Time, bool) {
	if purpose.Normalize() != PurposeMonthlyBilling || dueDate == nil {
		return time.Time{}, false
	}
	loc := billing.LocationBRT()
	return billing.CancelCutoff(dueDate.In(loc), loc, billing.DefaultCancelDeadlineDay), true
}

func (i *Invoice) AwaitingPayment() bool {
	if i == nil || i.Reserved() {
		return false
	}
	return i.Status == StatusPending || i.Status == StatusOverdue
}

func (i *Invoice) PaymentAt(now time.Time) PaymentView {
	closed := PaymentView{State: PaymentClosed}
	if !i.AwaitingPayment() || !i.paysByPix() {
		return closed
	}
	until, bounded := PayableUntil(i.Purpose, i.DueDate)
	if bounded && now.After(until) {
		return closed
	}

	deadline := i.chargeDeadline()
	if bounded && deadline != nil && deadline.After(until) {
		deadline = &until
	}
	live := deadline != nil && i.hasPixCode() && now.Add(ChargeExpiryMargin).Before(*deadline)

	switch {
	case live:
		view := PaymentView{State: PaymentOpen, PixQrCode: i.PixQrCode, PixCopy: i.PixCopy, ExpiresAt: deadline}
		if bounded {
			view.PayableUntil = &until
		}
		return view
	case bounded:
		return PaymentView{State: PaymentRenewable, PayableUntil: &until}
	default:
		return closed
	}
}

func (i *Invoice) RenewalDeadline(now time.Time) (time.Time, error) {
	view := i.PaymentAt(now)
	switch view.State {
	case PaymentOpen:
		return time.Time{}, ErrChargeStillLive
	case PaymentRenewable:
		return *view.PayableUntil, nil
	default:
		return time.Time{}, ErrNotPayable
	}
}

func RenewalIdempotencyKey(invoiceID, previousChargeID string) string {
	return ExternalReference(invoiceID) + ":" + previousChargeID
}

func (i *Invoice) paysByPix() bool {
	return strings.EqualFold(strings.TrimSpace(i.BillingType), string(payment.MethodPix))
}

func (i *Invoice) hasPixCode() bool {
	return i.PixCopy != nil && strings.TrimSpace(*i.PixCopy) != ""
}

func (i *Invoice) chargeDeadline() *time.Time {
	if i.ChargeExpiresAt != nil {
		deadline := *i.ChargeExpiresAt
		return &deadline
	}
	if i.Status != StatusPending {
		return nil
	}
	if i.DueDate != nil {
		deadline := *i.DueDate
		return &deadline
	}
	if i.CreatedAt.IsZero() {
		return nil
	}
	deadline := i.CreatedAt.Add(DefaultChargeLifetime)
	return &deadline
}
