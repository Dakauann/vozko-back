package workspace_pricing

const usdToBRLService = "usd_to_brl"

func FindPricingItem(items []PricingItem, category ServiceCategory, service, metric string) (PricingItem, bool) {
	for _, item := range items {
		if item.Category == category && item.Service == service && item.Metric == metric {
			return item, true
		}
	}
	return PricingItem{}, false
}

func USDToBRLMicros(items []PricingItem) (int64, bool) {
	item, ok := FindPricingItem(items, CategoryExchangeRate, usdToBRLService, "per_unit")
	if !ok || item.PriceMicros <= 0 {
		return 0, false
	}
	return item.PriceMicros, true
}

func ServiceMessageCostMicros(resolved []ResolvedPricingItem) int64 {
	if item := findResolvedItem(resolved, CategoryWhatsApp, WhatsAppServiceServiceMessage, "per_message"); item != nil {
		return max(item.CostMicros, 0)
	}
	return 0
}
