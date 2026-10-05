package workspace_pricing_usecase

import (
	"fmt"

	"github.com/google/uuid"

	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type updatePricingItemCostUseCase struct {
	repo workspace_pricing.Repository
}

func NewUpdatePricingItemCostUseCase(repo workspace_pricing.Repository) workspace_pricing.UpdatePricingItemCostUseCase {
	return &updatePricingItemCostUseCase{repo: repo}
}

func (uc *updatePricingItemCostUseCase) Execute(input workspace_pricing.UpdatePricingItemCostInput, changedBy string) (*workspace_pricing.PricingItem, error) {
	if !workspace_pricing.IsCategoryConfigurable(input.Category) {
		return nil, workspace_pricing.ErrCategoryNotConfigurable
	}
	if input.CostMicros <= 0 {
		return nil, workspace_pricing.ErrCostMicrosNotPositive
	}

	defaults, err := uc.repo.ListDefaultPricingItems()
	if err != nil {
		return nil, fmt.Errorf("failed to list defaults: %w", err)
	}
	item, ok := workspace_pricing.FindPricingItem(defaults, input.Category, input.Service, input.Metric)
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s/%s", workspace_pricing.ErrPricingItemNotFound, input.Category, input.Service, input.Metric)
	}

	oldCost := item.CostMicros
	item.CostMicros = input.CostMicros
	if err := uc.repo.UpsertPricingItem(&item); err != nil {
		return nil, fmt.Errorf("failed to upsert pricing item: %w", err)
	}

	_ = uc.repo.CreateAuditEntry(&workspace_pricing.PricingAuditEntry{
		ID:             uuid.New().String(),
		Category:       item.Category,
		Service:        item.Service,
		Metric:         item.Metric,
		OldPriceMicros: item.PriceMicros,
		NewPriceMicros: item.PriceMicros,
		OldCostMicros:  oldCost,
		NewCostMicros:  item.CostMicros,
		Currency:       item.Currency,
		ChangedBy:      changedBy,
	})

	return uc.repo.GetPricingItem(item.ID)
}
