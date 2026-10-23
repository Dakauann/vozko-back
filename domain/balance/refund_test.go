package balance

import (
	"errors"
	"testing"
)

func TestRefundAllowed(t *testing.T) {
	cases := []struct {
		name    string
		charged int64
		refund  int64
		want    error
	}{
		{name: "refunding the whole charge", charged: 3_500, refund: 3_500},
		{name: "refunding part of the charge", charged: 3_500, refund: 140},
		{name: "a refund with no charge behind it", charged: 0, refund: 140, want: ErrRefundWithoutCharge},
		{name: "a refund larger than the charge", charged: 3_500, refund: 3_501, want: ErrRefundExceedsCharge},
		{name: "a corrupt negative charge total", charged: -1, refund: 1, want: ErrRefundWithoutCharge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := RefundAllowed(tc.charged, tc.refund); !errors.Is(err, tc.want) && !(err == nil && tc.want == nil) {
				t.Fatalf("RefundAllowed(%d, %d) = %v, want %v", tc.charged, tc.refund, err, tc.want)
			}
		})
	}
}
