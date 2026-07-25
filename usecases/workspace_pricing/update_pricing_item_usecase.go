package workspace_pricing_usecase

import (
	"fmt"

	"github.com/google/uuid"

	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type updatePricingItemUseCase struct {
	repo workspace_pricing.Repository
}

func NewUpdatePricingItemUseCase(repo workspace_pricing.Repository) workspace_pricing.UpdatePricingItemUseCase {
	return &updatePricingItemUseCase{repo: repo}
}

func (uc *updatePricingItemUseCase) Execute(input workspace_pricing.UpdatePricingItemInput, changedBy string) (*workspace_pricing.PricingItem, error) {
	if !workspace_pricing.IsCategoryConfigurable(input.Category) {
		return nil, workspace_pricing.ErrCategoryNotConfigurable
	}

	if input.PriceMicros <= 0 {
		return nil, workspace_pricing.ErrPriceMicrosNotPositive
	}

	defaults, err := uc.repo.ListDefaultPricingItems()
	if err != nil {
		return nil, fmt.Errorf("failed to list defaults: %w", err)
	}

	var existing *workspace_pricing.PricingItem
	for i := range defaults {
		if defaults[i].Category == input.Category && defaults[i].Service == input.Service && defaults[i].Metric == input.Metric {
			existing = &defaults[i]
			break
		}
	}

	oldPrice := int64(0)
	if existing != nil {
		oldPrice = existing.PriceMicros
		existing.PriceMicros = input.PriceMicros
		existing.Currency = input.Currency
	} else {
		existing = &workspace_pricing.PricingItem{
			ID:          uuid.New().String(),
			Category:    input.Category,
			Service:     input.Service,
			Metric:      input.Metric,
			PriceMicros: input.PriceMicros,
			Currency:    input.Currency,
		}
	}

	if err := uc.repo.UpsertPricingItem(existing); err != nil {
		return nil, fmt.Errorf("failed to upsert pricing item: %w", err)
	}

	_ = uc.repo.CreateAuditEntry(&workspace_pricing.PricingAuditEntry{
		ID:             uuid.New().String(),
		Category:       input.Category,
		Service:        input.Service,
		Metric:         input.Metric,
		OldPriceMicros: oldPrice,
		NewPriceMicros: input.PriceMicros,
		Currency:       input.Currency,
		ChangedBy:      changedBy,
	})

	return uc.repo.GetPricingItem(existing.ID)
}
