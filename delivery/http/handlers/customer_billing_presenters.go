package handlers

import (
	"time"

	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

// This file is the single home for customer-facing billing presenters. Domain entities
// (PlanDefinition, AddonDefinition) legitimately carry our internal vendor COST and MARKUP because the
// domain and use-case layers price against them. Deciding what an END CUSTOMER's HTTP response may
// contain is a delivery-layer (presenter) concern, not a domain one, so it lives here.
//
// POLICY: a customer must only ever see the final PRICE, never our cost or markup. Every customer-facing
// endpoint that returns plans or addons MUST map through one of these presenters. Admin endpoints keep
// returning the raw entities. Centralizing the mappers here makes that policy auditable in one place and
// is guarded by the *_customer_dto_test.go tests.

// customerPlanPricingItem is the per-service pricing shown to end customers: the final PRICE only,
// never our internal vendor cost or markup.
type customerPlanPricingItem struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	Service     string `json:"service"`
	Metric      string `json:"metric"`
	PriceMicros int64  `json:"priceMicros"`
	Currency    string `json:"currency"`
}

// customerPlanDetails is the plan shape returned to end customers. It embeds the full PlanDefinition so
// every plan field is preserved, but its own pricingItems field shadows the entity's in JSON (both use
// the "pricingItems" tag, and the shallower field wins), so costMicros/markupPct never reach the
// browser. Build only via toCustomerPlanDetails.
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

// customerAddonDefinition is the addon-catalog shape returned to end customers. It is an EXPLICIT
// allowlist of fields (not an embed) so our internal monthlyCostMicros/annualCostMicros can never reach
// the browser: the customer sees only the final price. Build only via toCustomerAddonDefinitions.
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
