package container

import (
	"context"

	"vozko/domain/affiliate"
	"vozko/domain/workspace/workspace_pricing"
)

type affiliateExchangeRateAdapter struct {
	repo workspace_pricing.Repository
}

func newAffiliateExchangeRateAdapter(repo workspace_pricing.Repository) affiliate.ExchangeRateProvider {
	return &affiliateExchangeRateAdapter{repo: repo}
}

func (a *affiliateExchangeRateAdapter) CurrentRateMicros(_ context.Context) (int64, error) {
	if a == nil || a.repo == nil {
		return 0, affiliate.ErrExchangeRateUnavailable
	}
	items, err := a.repo.ListDefaultPricingItems()
	if err != nil {
		return 0, err
	}
	for i := range items {
		it := &items[i]
		if it.Category == workspace_pricing.CategoryExchangeRate &&
			it.Service == "usd_to_brl" &&
			it.PriceMicros > 0 {
			return it.PriceMicros, nil
		}
	}
	return 0, affiliate.ErrExchangeRateUnavailable
}
