package workspace_pricing

type staleDefaultCost struct {
	Category        ServiceCategory
	Service         string
	Metric          string
	StaleCostMicros int64
}

var staleDefaultCosts = []staleDefaultCost{
	{Category: CategoryEmail, Service: EmailServiceReceive, Metric: EmailMetricPerMessage, StaleCostMicros: 100},
}

func CorrectedDefaultCost(item PricingItem) (int64, bool) {
	for _, stale := range staleDefaultCosts {
		if stale.Category != item.Category || stale.Service != item.Service || stale.Metric != item.Metric || stale.StaleCostMicros != item.CostMicros {
			continue
		}
		for _, current := range DefaultPricingCatalog {
			if current.Category == item.Category && current.Service == item.Service && current.Metric == item.Metric && current.CostMicros != item.CostMicros {
				return current.CostMicros, true
			}
		}
	}
	return 0, false
}
