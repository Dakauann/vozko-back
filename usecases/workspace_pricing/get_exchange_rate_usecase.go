package workspace_pricing_usecase

import (
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type getExchangeRateUseCase struct {
	repo workspace_pricing.Repository
}

func NewGetExchangeRateUseCase(repo workspace_pricing.Repository) workspace_pricing.GetExchangeRateUseCase {
	return &getExchangeRateUseCase{repo: repo}
}

func (uc *getExchangeRateUseCase) Execute() (*workspace_pricing.PricingItem, error) {
	defaults, err := uc.repo.ListDefaultPricingItems()
	if err != nil {
		return nil, err
	}
	for i := range defaults {
		if defaults[i].Category == workspace_pricing.CategoryExchangeRate &&
			defaults[i].Service == "usd_to_brl" {
			return &defaults[i], nil
		}
	}
	fallback := workspace_pricing.DefaultPricingCatalog[len(workspace_pricing.DefaultPricingCatalog)-1]
	return &fallback, nil
}
