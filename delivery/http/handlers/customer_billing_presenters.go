package handlers

import (
	"time"

	workspace_addon "vozko/domain/workspace/workspace_addon"
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

type customerAddonDefinition struct {
	ID                 string                          `json:"id"`
	Key                string                          `json:"key"`
	Name               string                          `json:"name"`
	Description        string                          `json:"description"`
	EntitlementKind    workspace_addon.EntitlementKind `json:"entitlementKind"`
	UnitsPerQuantity   int                             `json:"unitsPerQuantity"`
	MonthlyPriceMicros int64                           `json:"monthlyPriceMicros"`
	AnnualPriceMicros  int64                           `json:"annualPriceMicros"`
	IsActive           bool                            `json:"isActive"`
	IsGloballyVisible  bool                            `json:"isGloballyVisible"`
	ArchivedAt         *time.Time                      `json:"archivedAt,omitempty"`
	CreatedAt          time.Time                       `json:"createdAt"`
	UpdatedAt          time.Time                       `json:"updatedAt"`
}

func toCustomerAddonDefinitions(defs []*workspace_addon.AddonDefinition) []customerAddonDefinition {
	out := make([]customerAddonDefinition, 0, len(defs))
	for _, d := range defs {
		out = append(out, customerAddonDefinition{
			ID:                 d.ID,
			Key:                d.Key,
			Name:               d.Name,
			Description:        d.Description,
			EntitlementKind:    d.EntitlementKind,
			UnitsPerQuantity:   d.UnitsPerQuantity,
			MonthlyPriceMicros: d.MonthlyPriceMicros,
			AnnualPriceMicros:  d.AnnualPriceMicros,
			IsActive:           d.IsActive,
			IsGloballyVisible:  d.IsGloballyVisible,
			ArchivedAt:         d.ArchivedAt,
			CreatedAt:          d.CreatedAt,
			UpdatedAt:          d.UpdatedAt,
		})
	}
	return out
}
