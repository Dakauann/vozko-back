package workspace_pricing

import "testing"

func TestTheExchangeRateComesFromTheGlobalCatalog(t *testing.T) {
	items := []PricingItem{{Category: CategoryExchangeRate, Service: "usd_to_brl", Metric: "per_unit", PriceMicros: 5_420_000}}
	if got, ok := USDToBRLMicros(items); !ok || got != 5_420_000 {
		t.Fatalf("exchange rate %d %v", got, ok)
	}
}

func TestAZeroExchangeRateIsReportedAsMissing(t *testing.T) {
	if _, ok := USDToBRLMicros([]PricingItem{{Category: CategoryExchangeRate, Service: "usd_to_brl", Metric: "per_unit"}}); ok {
		t.Fatal("a zero exchange rate is not a rate")
	}
}

func TestTheServiceMessageCostComesFromTheResolvedList(t *testing.T) {
	resolved := ResolvePricingLayers(
		[]PricingItem{{Category: CategoryWhatsApp, Service: WhatsAppServiceServiceMessage, Metric: "per_message", CostMicros: 4_000}},
		[]PricingItem{{Category: CategoryWhatsApp, Service: WhatsAppServiceServiceMessage, Metric: "per_message", CostMicros: 3_000}},
	)
	if got := ServiceMessageCostMicros(resolved); got != 3_000 {
		t.Fatalf("a plan cost above zero wins, got %d", got)
	}
	if got := ServiceMessageCostMicros(nil); got != 0 {
		t.Fatalf("no row means no cost, got %d", got)
	}
}
