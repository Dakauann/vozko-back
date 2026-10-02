package workspace_pricing

import (
	"errors"
	"testing"
)

func TestPublishedAdFeeIsAConfigurableCatalogLine(t *testing.T) {
	item := catalogEntry(t, CategoryAdvertising, AdvertisingServicePublishedAd, "per_ad")
	if item.PriceMicros <= 0 || item.Currency != "USD" {
		t.Fatalf("catalog fee %+v must be priced in USD like the other lines", item)
	}
	if !IsCategoryConfigurable(CategoryAdvertising) {
		t.Fatal("admins cannot change the ad fee")
	}
}

func TestExchangeRateStaysTheLastCatalogLine(t *testing.T) {
	last := DefaultPricingCatalog[len(DefaultPricingCatalog)-1]
	if last.Category != CategoryExchangeRate {
		t.Fatalf("last catalog line is %s; the exchange rate fallback reads the last line", last.Category)
	}
}

func TestPublishedAdFeeFollowsThePlanOverride(t *testing.T) {
	defaults := stubDefaults{items: []PricingItem{{Category: CategoryAdvertising, Service: AdvertisingServicePublishedAd, Metric: "per_ad", PriceMicros: 1_000_000, Currency: "USD"}}}
	plan := stubPlanPricing{items: []PricingItem{{Category: CategoryAdvertising, Service: AdvertisingServicePublishedAd, Metric: "per_ad", PriceMicros: 2_500_000, Currency: "USD"}}}
	got, err := NewPricer(defaults, WithPlanPricingProvider(plan)).PriceAdvertising("ws-1", AdvertisingServicePublishedAd)
	if err != nil || got.PriceMicros != 2_500_000 || got.ProfitMicros != 2_500_000 {
		t.Fatalf("fee %+v, %v", got, err)
	}
}

func TestUnpricedAdFeeIsRefusedNotFree(t *testing.T) {
	defaults := stubDefaults{items: []PricingItem{{Category: CategoryAdvertising, Service: AdvertisingServicePublishedAd, Metric: "per_ad", Currency: "USD"}}}
	if _, err := NewPricer(defaults).PriceAdvertising("ws-1", AdvertisingServicePublishedAd); !errors.Is(err, ErrPricingItemNotFound) {
		t.Fatalf("zero price err = %v, want ErrPricingItemNotFound", err)
	}
	if _, err := NewPricer(stubDefaults{}).PriceAdvertising("ws-1", AdvertisingServicePublishedAd); !errors.Is(err, ErrPricingItemNotFound) {
		t.Fatalf("missing line err = %v, want ErrPricingItemNotFound", err)
	}
}
