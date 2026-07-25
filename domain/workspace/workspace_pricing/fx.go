package workspace_pricing

// DefaultUSDToBRL is the fallback exchange rate used when none is configured.
const DefaultUSDToBRL = 6.0

// USDToBRLRate resolves the USD->BRL rate from the default pricing items (category exchange_rate,
// service usd_to_brl), falling back to DefaultUSDToBRL when absent or non-positive. It is the single
// source of the rate so invoice composition and balance crediting never drift apart.
func USDToBRLRate(items []PricingItem) float64 {
	for _, it := range items {
		if it.Category == CategoryExchangeRate && it.Service == "usd_to_brl" && it.PriceMicros > 0 {
			return float64(it.PriceMicros) / 1_000_000
		}
	}
	return DefaultUSDToBRL
}
