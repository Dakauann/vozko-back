package template

import (
	"errors"
	"math"
	"testing"
)

func TestQuote(t *testing.T) {
	cases := []struct {
		name                    string
		unit, eligible, balance int64
		currency                string
		wantCost                int64
		wantAffordable          bool
		wantErr                 error
	}{
		{"one send within balance", 66_667, 1, 100_000, "USD", 66_667, true, nil},
		{"bulk send above balance", 66_667, 2, 100_000, "USD", 133_334, false, nil},
		{"balance exactly covers the cost", 50_000, 3, 150_000, "USD", 150_000, true, nil},
		{"nobody eligible costs nothing", 50_000, 0, 0, "USD", 0, true, nil},
		{"negative balance never affords", 50_000, 0, -1, "USD", 0, false, nil},
		{"missing price", 0, 10, 1_000_000, "USD", 0, false, ErrPricingUnavailable},
		{"negative price", -5, 10, 1_000_000, "USD", 0, false, ErrPricingUnavailable},
		{"negative eligible", 50_000, -1, 1_000_000, "USD", 0, false, ErrQuoteOutOfRange},
		{"overflowing cost", math.MaxInt64 / 2, 3, math.MaxInt64, "USD", 0, false, ErrQuoteOutOfRange},
		{"missing currency", 50_000, 1, 1_000_000, " ", 0, false, ErrBillingNotConfigured},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := Quote(tc.unit, tc.eligible, tc.balance, tc.currency)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
				if q.Affordable {
					t.Fatal("a refused quote must never read as affordable")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if q.CostMicros != tc.wantCost || q.Affordable != tc.wantAffordable || q.UnitPriceMicros != tc.unit ||
				q.Eligible != tc.eligible || q.BalanceMicros != tc.balance || q.Currency != tc.currency {
				t.Fatalf("unexpected quote %+v", q)
			}
		})
	}
}
