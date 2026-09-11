package workspace_pricing

import (
	"errors"
	"testing"
)

type stubPricingRepo struct {
	Repository
	items []PricingItem
	err   error
}

func (s stubPricingRepo) ListDefaultPricingItems() ([]PricingItem, error) { return s.items, s.err }

// The per-comment surcharge is OPTIONAL (plan §9.2). A missing or zero
// item means "token billing only", which is a deliberate state: it must
// price to zero with no error, unlike a missing telephony rate.
func TestPriceAudience_UnconfiguredIsZeroNotError(t *testing.T) {
	p := NewPricer(stubPricingRepo{items: DefaultPricingCatalog})
	got, err := p.PriceAudience("ws-1", 20)
	if err != nil {
		t.Fatalf("unconfigured surcharge must not be an error: %v", err)
	}
	if got.PriceMicros != 0 || got.CostMicros != 0 {
		t.Fatalf("unconfigured surcharge must price to zero, got %+v", got)
	}
}

func TestPriceAudience_MultipliesByComments(t *testing.T) {
	// An operator priced the seeded row: same key, now with a price.
	items := append([]PricingItem{}, DefaultPricingCatalog...)
	for i := range items {
		if items[i].Service == AudienceService {
			items[i].CostMicros, items[i].PriceMicros = 100, 500
		}
	}
	p := NewPricer(stubPricingRepo{items: items})
	got, err := p.PriceAudience("ws-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if got.PriceMicros != 10_000 || got.CostMicros != 2_000 || got.ProfitMicros != 8_000 {
		t.Fatalf("20 comments at 500 = %+v", got)
	}
	// Nothing analysed, nothing charged.
	if got, _ := p.PriceAudience("ws-1", 0); got.PriceMicros != 0 {
		t.Fatalf("zero comments priced %+v", got)
	}
}

func TestPriceAudience_RepositoryErrorPropagates(t *testing.T) {
	p := NewPricer(stubPricingRepo{err: errors.New("db down")})
	if _, err := p.PriceAudience("ws-1", 1); err == nil {
		t.Fatal("a repository failure must surface, not price to zero")
	}
}

// The catalog seeds the row at zero so operators find it in the pricing
// admin and can switch it on, rather than having to know the key.
func TestDefaultCatalogCarriesCommentAnalysisAtZero(t *testing.T) {
	for _, it := range DefaultPricingCatalog {
		if it.Category == CategoryLLM && it.Service == AudienceService && it.Metric == AudienceMetric {
			if it.PriceMicros != 0 {
				t.Fatalf("the seeded surcharge must default to 0 (token billing only), got %d", it.PriceMicros)
			}
			return
		}
	}
	t.Fatal("DefaultPricingCatalog is missing the audience/per_comment item")
}
