package balance

import "errors"

var (
	ErrRefundWithoutCharge = errors.New("a refund needs a recorded charge to reverse")
	ErrRefundExceedsCharge = errors.New("a refund cannot be larger than the charge it reverses")
)

func RefundAllowed(chargedMicros, refundMicros int64) error {
	if chargedMicros <= 0 {
		return ErrRefundWithoutCharge
	}
	if refundMicros > chargedMicros {
		return ErrRefundExceedsCharge
	}
	return nil
}
