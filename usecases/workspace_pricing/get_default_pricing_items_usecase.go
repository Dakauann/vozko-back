package workspace_pricing_usecase

import (
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type getDefaultPricingItemsUseCase struct {
	repo workspace_pricing.Repository
}

func NewGetDefaultPricingItemsUseCase(repo workspace_pricing.Repository) workspace_pricing.GetDefaultPricingItemsUseCase {
	return &getDefaultPricingItemsUseCase{repo: repo}
}

func (uc *getDefaultPricingItemsUseCase) Execute() ([]workspace_pricing.PricingItem, error) {
	return uc.repo.ListDefaultPricingItems()
}
