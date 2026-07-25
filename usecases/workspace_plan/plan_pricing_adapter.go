package workspace_plan_usecase

import (
	"time"

	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type PlanPricingAdapter struct {
	subReader workspace_plan.CurrentSubscriptionReader
	planRepo  workspace_plan.PlanRepository
}

func NewPlanPricingAdapter(
	subReader workspace_plan.CurrentSubscriptionReader,
	planRepo workspace_plan.PlanRepository,
) workspace_pricing.PlanPricingProvider {
	return &PlanPricingAdapter{subReader: subReader, planRepo: planRepo}
}

func (a *PlanPricingAdapter) ListForWorkspace(workspaceID string) ([]workspace_pricing.PricingItem, error) {
	sub, err := a.subReader.GetCurrentByWorkspaceID(workspaceID, time.Now())
	if err != nil {

		return nil, nil
	}
	items, err := a.planRepo.ListPricingItems(sub.PlanDefinitionID)
	if err != nil {
		return nil, err
	}
	result := make([]workspace_pricing.PricingItem, len(items))
	for i, item := range items {
		result[i] = workspace_pricing.PricingItem{
			Category:    workspace_pricing.ServiceCategory(item.Category),
			Service:     item.Service,
			Metric:      item.Metric,
			CostMicros:  item.CostMicros,
			PriceMicros: item.PriceMicros,
			MarkupPct:   item.MarkupPct,
			Currency:    item.Currency,
		}
	}
	return result, nil
}
