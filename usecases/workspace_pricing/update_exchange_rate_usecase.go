package workspace_pricing_usecase

import (
	"fmt"

	"github.com/google/uuid"

	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type updateExchangeRateUseCase struct {
	repo workspace_pricing.Repository
}

func NewUpdateExchangeRateUseCase(repo workspace_pricing.Repository) workspace_pricing.UpdateExchangeRateUseCase {
	return &updateExchangeRateUseCase{repo: repo}
}

func (uc *updateExchangeRateUseCase) Execute(priceMicros int64, changedBy string) (*workspace_pricing.PricingItem, error) {
	defaults, err := uc.repo.ListDefaultPricingItems()
	if err != nil {
		return nil, fmt.Errorf("failed to list defaults: %w", err)
	}

	var existing *workspace_pricing.PricingItem
	for i := range defaults {
		if defaults[i].Category == workspace_pricing.CategoryExchangeRate &&
			defaults[i].Service == "usd_to_brl" &&
			defaults[i].Metric == "per_unit" {
			existing = &defaults[i]
			break
		}
	}

	oldPrice := int64(0)
	if existing != nil {
		oldPrice = existing.PriceMicros
		existing.PriceMicros = priceMicros
	} else {
		existing = &workspace_pricing.PricingItem{
			ID:          uuid.New().String(),
			Category:    workspace_pricing.CategoryExchangeRate,
			Service:     "usd_to_brl",
			Metric:      "per_unit",
			PriceMicros: priceMicros,
			Currency:    "BRL",
		}
	}

	if err := uc.repo.UpsertPricingItem(existing); err != nil {
		return nil, fmt.Errorf("failed to update exchange rate: %w", err)
	}

	_ = uc.repo.CreateAuditEntry(&workspace_pricing.PricingAuditEntry{
		ID:             uuid.New().String(),
		Category:       workspace_pricing.CategoryExchangeRate,
		Service:        "usd_to_brl",
		Metric:         "per_unit",
		OldPriceMicros: oldPrice,
		NewPriceMicros: priceMicros,
		Currency:       "BRL",
		ChangedBy:      changedBy,
	})

	return uc.repo.GetPricingItem(existing.ID)
}
