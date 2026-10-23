package workspace_pricing

import (
	"errors"
	"testing"
)

func TestEmailCatalogLines(t *testing.T) {
	cases := []struct {
		line  EmailPriceLine
		price int64
		cost  int64
	}{
		{line: EmailSendPerMessage, price: 583, cost: 165},
		{line: EmailSendPerChunk, price: 102, cost: 29},
		{line: EmailReceivePerMessage, price: 583, cost: 190},
		{line: EmailReceivePerChunk, price: 315, cost: 90},
	}
	for _, tc := range cases {
		item := catalogEntry(t, CategoryEmail, tc.line.Service, tc.line.Metric)
		if item.PriceMicros != tc.price || item.CostMicros != tc.cost || item.Currency != "USD" {
			t.Errorf("%s/%s = %+v", tc.line.Service, tc.line.Metric, item)
		}
	}
	if !IsCategoryConfigurable(CategoryEmail) {
		t.Fatal("admins cannot change the email prices")
	}
	for _, line := range DefaultPricingCatalog {
		if line.Category == CategoryEmail && !(EmailPriceLine{Service: line.Service, Metric: line.Metric}).Valid() {
			t.Errorf("retired email line %s/%s is still in the catalog", line.Service, line.Metric)
		}
	}
}

func TestEmailPriceLineValid(t *testing.T) {
	for _, line := range []EmailPriceLine{EmailSendPerMessage, EmailSendPerChunk, EmailReceivePerMessage, EmailReceivePerChunk} {
		if !line.Valid() {
			t.Fatalf("%+v is a priced email line", line)
		}
	}
	for _, retired := range []EmailPriceLine{{Service: "validation", Metric: "per_address"}, {Service: EmailServiceReceive, Metric: "per_address"}, {Service: "fax", Metric: EmailMetricPerMessage}} {
		if retired.Valid() {
			t.Fatalf("%+v is not a priced email line", retired)
		}
	}
}

func TestARetiredEmailOverrideIsNeverResolved(t *testing.T) {
	overrides := []PricingItem{
		{ID: "o1", Category: CategoryEmail, Service: "validation", Metric: "per_address", PriceMicros: 13_333, Currency: "USD"},
	}
	for _, r := range ResolvePricingLayers(DefaultPricingCatalog, overrides) {
		if r.Category == CategoryEmail && r.Service == "validation" {
			t.Fatalf("a retired line resolved: %+v", r)
		}
	}
}

func emailDefaults() stubDefaults {
	return stubDefaults{items: []PricingItem{
		{Category: CategoryEmail, Service: EmailServiceSend, Metric: EmailMetricPerMessage, CostMicros: 165, PriceMicros: 583, Currency: "USD"},
		{Category: CategoryEmail, Service: EmailServiceSend, Metric: EmailMetricPerChunk, CostMicros: 29, PriceMicros: 102, Currency: "USD"},
		{Category: CategoryEmail, Service: EmailServiceReceive, Metric: EmailMetricPerMessage, CostMicros: 190, PriceMicros: 583, Currency: "USD"},
		{Category: CategoryEmail, Service: EmailServiceReceive, Metric: EmailMetricPerChunk, CostMicros: 90, PriceMicros: 315, Currency: "USD"},
	}}
}

func TestPriceEmailFollowsThePlanOverride(t *testing.T) {
	plan := stubPlanPricing{items: []PricingItem{
		{Category: CategoryEmail, Service: EmailServiceSend, Metric: EmailMetricPerMessage, PriceMicros: 400, Currency: "USD"},
		{Category: CategoryEmail, Service: EmailServiceReceive, Metric: EmailMetricPerChunk, PriceMicros: 200, Currency: "USD"},
	}}
	got, err := NewPricer(emailDefaults(), WithPlanPricingProvider(plan)).PriceEmail("ws-1", EmailSendPerMessage)
	if err != nil || got.PriceMicros != 400 || got.CostMicros != 165 || got.ProfitMicros != 235 {
		t.Fatalf("send = %+v, %v", got, err)
	}
	got, err = NewPricer(emailDefaults(), WithPlanPricingProvider(plan)).PriceEmail("ws-1", EmailReceivePerChunk)
	if err != nil || got.PriceMicros != 200 || got.CostMicros != 90 {
		t.Fatalf("receive chunk = %+v, %v", got, err)
	}
	got, err = NewPricer(emailDefaults()).PriceEmail("ws-1", EmailReceivePerMessage)
	if err != nil || got.PriceMicros != 583 || got.CostMicros != 190 {
		t.Fatalf("default receive = %+v, %v", got, err)
	}
}

func TestUnpricedEmailIsRefusedNotFree(t *testing.T) {
	zero := stubDefaults{items: []PricingItem{{Category: CategoryEmail, Service: EmailServiceSend, Metric: EmailMetricPerMessage, Currency: "USD"}}}
	cases := []struct {
		name   string
		pricer Pricer
		line   EmailPriceLine
		want   error
	}{
		{"zero price", NewPricer(zero), EmailSendPerMessage, ErrPricingItemNotFound},
		{"missing line", NewPricer(stubDefaults{}), EmailReceivePerChunk, ErrPricingItemNotFound},
		{"retired service", NewPricer(emailDefaults()), EmailPriceLine{Service: "validation", Metric: "per_address"}, ErrEmailServiceUnsupported},
		{"unknown service", NewPricer(emailDefaults()), EmailPriceLine{Service: "fax", Metric: EmailMetricPerMessage}, ErrEmailServiceUnsupported},
		{"plan lookup fails", NewPricer(emailDefaults(), WithPlanPricingProvider(stubPlanPricing{err: errors.New("db down")})), EmailSendPerMessage, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.pricer.PriceEmail("ws-1", tc.line)
			if err == nil || got.PriceMicros != 0 {
				t.Fatalf("PriceEmail = %+v, %v", got, err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTheReceiveCostCountsTheChunkEveryEmailIsBilledFor(t *testing.T) {
	message := catalogEntry(t, CategoryEmail, EmailServiceReceive, EmailMetricPerMessage)
	chunk := catalogEntry(t, CategoryEmail, EmailServiceReceive, EmailMetricPerChunk)
	if message.CostMicros != 100+chunk.CostMicros {
		t.Fatalf("receive per message cost = %d, want the 100 per email plus the %d of the first chunk", message.CostMicros, chunk.CostMicros)
	}
}

func TestTheStaleReceiveCostIsCorrected(t *testing.T) {
	stale := PricingItem{Category: CategoryEmail, Service: EmailServiceReceive, Metric: EmailMetricPerMessage, CostMicros: 100, PriceMicros: 583}
	cost, ok := CorrectedDefaultCost(stale)
	if !ok || cost != 190 {
		t.Fatalf("CorrectedDefaultCost(stale) = %d, %v", cost, ok)
	}
	edited := stale
	edited.CostMicros = 150
	if _, ok := CorrectedDefaultCost(edited); ok {
		t.Fatal("a cost an admin edited is never overwritten")
	}
	current := stale
	current.CostMicros = 190
	if _, ok := CorrectedDefaultCost(current); ok {
		t.Fatal("a corrected cost needs no correction")
	}
	other := stale
	other.Service = EmailServiceSend
	if _, ok := CorrectedDefaultCost(other); ok {
		t.Fatal("only the listed line is corrected")
	}
	for _, c := range staleDefaultCosts {
		entry := catalogEntry(t, c.Category, c.Service, c.Metric)
		if entry.CostMicros == c.StaleCostMicros {
			t.Fatalf("%s/%s/%s still holds its stale cost", c.Category, c.Service, c.Metric)
		}
	}
}
