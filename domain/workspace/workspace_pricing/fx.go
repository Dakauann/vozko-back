package workspace_pricing

const DefaultUSDToBRL = 6.0

func USDToBRLRate(items []PricingItem) float64 {
	for _, it := range items {
		if it.Category == CategoryExchangeRate && it.Service == "usd_to_brl" && it.PriceMicros > 0 {
			return float64(it.PriceMicros) / 1_000_000
		}
	}
	return DefaultUSDToBRL
}
