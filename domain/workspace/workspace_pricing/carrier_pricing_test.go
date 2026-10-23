package workspace_pricing

import (
	"errors"
	"testing"

	"vozko/domain/carriermessaging"
)

func TestEveryCarrierChargeHasAConfigurableCatalogLine(t *testing.T) {
	categories := map[carriermessaging.Channel]ServiceCategory{carriermessaging.ChannelRCS: CategoryRCS, carriermessaging.ChannelSMS: CategorySMS}
	for _, kind := range carriermessaging.AllChargeKinds() {
		category := categories[kind.Channel()]
		item := catalogEntry(t, category, kind.PricingService(), kind.PricingMetric())
		if item.PriceMicros <= 0 || item.CostMicros <= 0 || item.Currency != "USD" {
			t.Errorf("%s line = %+v", kind, item)
		}
		if !IsCategoryConfigurable(category) {
			t.Errorf("admins cannot change %s prices", category)
		}
	}
}

func TestCarrierMetricFor(t *testing.T) {
	if m, err := CarrierMetricFor(carriermessaging.ChannelRCS, "conversation"); err != nil || m != "per_conversation" {
		t.Fatalf("rcs conversation = %q, %v", m, err)
	}
	if m, err := CarrierMetricFor(carriermessaging.ChannelSMS, "segment"); err != nil || m != "per_segment" {
		t.Fatalf("sms segment = %q, %v", m, err)
	}
	if _, err := CarrierMetricFor(carriermessaging.ChannelSMS, "conversation"); !errors.Is(err, ErrCarrierServiceUnsupported) {
		t.Fatalf("sms has no conversation: %v", err)
	}
}

func carrierDefaults() stubDefaults {
	return stubDefaults{items: []PricingItem{
		{Category: CategoryRCS, Service: "basic", Metric: "per_message", CostMicros: 19_317, PriceMicros: 31_667, Currency: "USD"},
		{Category: CategorySMS, Service: "segment", Metric: "per_segment", CostMicros: 30_000, PriceMicros: 41_667, Currency: "USD"},
	}}
}

func TestCarrierPricesFollowThePlanOverride(t *testing.T) {
	plan := stubPlanPricing{items: []PricingItem{{Category: CategoryRCS, Service: "basic", Metric: "per_message", PriceMicros: 25_000, Currency: "USD"}}}
	got, err := NewPricer(carrierDefaults(), WithPlanPricingProvider(plan)).PriceRCS("ws-1", "basic")
	if err != nil || got.PriceMicros != 25_000 || got.CostMicros != 19_317 {
		t.Fatalf("rcs basic = %+v, %v", got, err)
	}
	got, err = NewPricer(carrierDefaults()).PriceSMS("ws-1", "segment")
	if err != nil || got.PriceMicros != 41_667 {
		t.Fatalf("sms segment = %+v, %v", got, err)
	}
}

func TestUnpricedCarrierMessagesAreRefusedNotFree(t *testing.T) {
	if _, err := NewPricer(carrierDefaults()).PriceSMS("ws-1", "inbound"); !errors.Is(err, ErrPricingItemNotFound) {
		t.Fatalf("missing line = %v", err)
	}
	if _, err := NewPricer(carrierDefaults()).PriceRCS("ws-1", "fax"); !errors.Is(err, ErrCarrierServiceUnsupported) {
		t.Fatalf("unknown service = %v", err)
	}
}
