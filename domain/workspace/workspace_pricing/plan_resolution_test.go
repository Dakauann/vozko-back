package workspace_pricing

import (
	"errors"
	"testing"
)

type stubDefaults struct {
	Repository
	items []PricingItem
}

func (s stubDefaults) ListDefaultPricingItems() ([]PricingItem, error) { return s.items, nil }

type stubPlanPricing struct {
	items []PricingItem
	err   error
}

func (s stubPlanPricing) ListForWorkspace(string) ([]PricingItem, error) { return s.items, s.err }

func sipDefaults() stubDefaults {
	return stubDefaults{items: []PricingItem{{Category: CategoryTelephony, Service: TelephonyServiceSIPCalls, Metric: "per_minute", Currency: "USD"}}}
}

func TestPlanPricesSIPMinutesInWholeMinutes(t *testing.T) {
	plan := stubPlanPricing{items: []PricingItem{{Category: CategoryTelephony, Service: TelephonyServiceSIPCalls, Metric: "per_minute", CostMicros: 1_000, PriceMicros: 4_000, Currency: "USD"}}}
	pricer := NewPricer(sipDefaults(), WithPlanPricingProvider(plan))
	cases := []struct {
		seconds float64
		minutes int64
	}{{1, 1}, {60, 1}, {61, 2}, {125, 3}}
	for _, tc := range cases {
		got, err := pricer.PriceTelephonyChannel("ws-1", tc.seconds, TelephonyChannelSIP)
		if err != nil {
			t.Fatalf("PriceTelephonyChannel(%v) error = %v", tc.seconds, err)
		}
		if got.PriceMicros != tc.minutes*4_000 || got.CostMicros != tc.minutes*1_000 {
			t.Errorf("%vs priced %d/%d, want %d minutes at the plan rate", tc.seconds, got.PriceMicros, got.CostMicros, tc.minutes)
		}
	}
}

func TestPricingFailsClosedWhenThePlanCannotBeRead(t *testing.T) {
	pricer := NewPricer(sipDefaults(), WithPlanPricingProvider(stubPlanPricing{err: errors.New("database unavailable")}))
	if _, err := pricer.PriceTelephonyChannel("ws-1", 60, TelephonyChannelSIP); err == nil {
		t.Fatal("pricing with an unreadable plan returned the catalog default, want an error")
	}
}

func TestPricingRejectsAnUnknownTelephonyChannel(t *testing.T) {
	pricer := NewPricer(sipDefaults())
	if _, err := pricer.PriceTelephonyChannel("ws-1", 60, "skype"); !errors.Is(err, ErrPricingItemNotFound) {
		t.Fatalf("unknown channel error = %v, want ErrPricingItemNotFound", err)
	}
}
