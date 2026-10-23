package workspace_pricing

func PriceOf(resolved []ResolvedPricingItem, category ServiceCategory, service, metric string) (PriceResult, bool) {
	item := findResolvedItem(resolved, category, service, metric)
	if item == nil || item.PriceMicros <= 0 {
		return PriceResult{}, false
	}
	return PriceResult{
		CostMicros:   item.CostMicros,
		PriceMicros:  item.PriceMicros,
		ProfitMicros: item.PriceMicros - item.CostMicros,
	}, true
}
