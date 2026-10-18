package handlers

import (
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type customerPlanPricingItem struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	Service     string `json:"service"`
	Metric      string `json:"metric"`
	PriceMicros int64  `json:"priceMicros"`
	Currency    string `json:"currency"`
}

type customerPlanDetails struct {
	*workspace_plan.PlanDefinition
	PricingItems []customerPlanPricingItem `json:"pricingItems"`
}

func toCustomerPlanDetails(p *workspace_plan.PlanDefinition) customerPlanDetails {
	items := make([]customerPlanPricingItem, 0, len(p.PricingItems))
	for _, it := range p.PricingItems {
		items = append(items, customerPlanPricingItem{
			ID:          it.ID,
			Category:    it.Category,
			Service:     it.Service,
			Metric:      it.Metric,
			PriceMicros: it.PriceMicros,
			Currency:    it.Currency,
		})
	}
	return customerPlanDetails{PlanDefinition: p, PricingItems: items}
}
