package workspace_plan

import "testing"

var catalog = []CatalogEntry{
	{Category: "whatsapp", Service: "marketing", Metric: "per_message", PriceMicros: 66_667},
	{Category: "telephony", Service: "sip_calls", Metric: "per_minute"},
	{Category: "exchange_rate", Service: "usd_to_brl", Metric: "per_unit", PriceMicros: 6_000_000},
}

func TestAPlanPricesExactlyTheCatalogsServices(t *testing.T) {
	stored := []PlanPricingItem{
		{ID: "kept", Category: "whatsapp", Service: "marketing", Metric: "per_message", PriceMicros: 70_000},
		{ID: "retired", Category: "telephony", Service: "sip_trunk", Metric: "per_minute", PriceMicros: 8_333},
		{ID: "rate", Category: "exchange_rate", Service: "usd_to_brl", Metric: "per_unit"},
	}

	merged := MergePricingItemsWithCatalog("plan-1", stored, catalog)

	if len(merged) != 2 {
		t.Fatalf("merged = %+v, want the stored marketing price and the catalog's SIP line", merged)
	}
	if merged[0].ID != "kept" || merged[0].PriceMicros != 70_000 {
		t.Fatalf("the plan's own price was lost: %+v", merged[0])
	}
	if merged[1].Service != "sip_calls" || merged[1].PlanDefinitionID != "plan-1" {
		t.Fatalf("the missing catalog line was not added: %+v", merged[1])
	}
}
