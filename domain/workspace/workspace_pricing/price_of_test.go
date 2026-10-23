package workspace_pricing

import "testing"

func TestPriceOfFindsTheResolvedItem(t *testing.T) {
	resolved := []ResolvedPricingItem{
		{Category: "rcs", Service: "basic", Metric: "per_message", CostMicros: 19_000, PriceMicros: 31_667},
		{Category: "rcs", Service: "single", Metric: "per_message", CostMicros: 38_000, PriceMicros: 0},
	}
	got, ok := PriceOf(resolved, "rcs", "basic", "per_message")
	if !ok || got != (PriceResult{CostMicros: 19_000, PriceMicros: 31_667, ProfitMicros: 12_667}) {
		t.Fatalf("PriceOf = %+v, %v", got, ok)
	}
	if _, ok := PriceOf(resolved, "rcs", "single", "per_message"); ok {
		t.Fatal("a zero price is not a price")
	}
	if _, ok := PriceOf(resolved, "rcs", "sms", "per_message"); ok {
		t.Fatal("a missing item is not a price")
	}
}
