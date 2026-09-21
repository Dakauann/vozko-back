package workspace_pricing

import "testing"

type stubPricingRepo struct{ Repository }

func (stubPricingRepo) ListDefaultPricingItems() ([]PricingItem, error) { return nil, nil }

type stubLLMFetcher struct{ in, out int64 }

func (s stubLLMFetcher) FetchLLMPriceMicros(string) (int64, int64, error) {
	return s.in, s.out, nil
}

func newPricer(f LLMPriceFetcher) Pricer {
	return NewPricer(stubPricingRepo{}, WithLLMPriceFetcher(f))
}

func TestProviderCostWinsOverTheTokenEstimate(t *testing.T) {
	p := newPricer(stubLLMFetcher{in: 1_000_000, out: 1_000_000})

	got, err := p.PriceLLM("ws-1", "deepseek/deepseek-v4-pro", 1_000_000, 0, 1_600_000)
	if err != nil {
		t.Fatalf("PriceLLM: %v", err)
	}
	if got.CostMicros != 1_600_000 {
		t.Errorf("CostMicros = %d, want the provider's 1600000 — billing the estimate loses the difference on every call", got.CostMicros)
	}
	if got.PriceMicros != 1_920_000 {
		t.Errorf("PriceMicros = %d, want 1920000 (cost + 20%%)", got.PriceMicros)
	}
	if got.ProfitMicros != got.PriceMicros-got.CostMicros {
		t.Errorf("profit %d does not reconcile with price %d - cost %d", got.ProfitMicros, got.PriceMicros, got.CostMicros)
	}
}

func TestMarkupAppliesToTheProviderCost(t *testing.T) {
	p := newPricer(stubLLMFetcher{in: 10, out: 10})

	got, err := p.PriceLLM("ws-1", "m", 1000, 1000, 500_000)
	if err != nil {
		t.Fatalf("PriceLLM: %v", err)
	}
	if got.CostMicros != 500_000 || got.PriceMicros != 600_000 {
		t.Errorf("got cost=%d price=%d, want 500000/600000", got.CostMicros, got.PriceMicros)
	}
}

func TestAbsentProviderCostFallsBackToTokens(t *testing.T) {
	p := newPricer(stubLLMFetcher{in: 1_000_000, out: 2_000_000})

	got, err := p.PriceLLM("ws-1", "m", 1_000_000, 500_000, 0)
	if err != nil {
		t.Fatalf("PriceLLM: %v", err)
	}
	if got.CostMicros != 2_000_000 {
		t.Errorf("CostMicros = %d, want the token estimate 2000000", got.CostMicros)
	}
	if got.PriceMicros != 2_400_000 {
		t.Errorf("PriceMicros = %d, want 2400000", got.PriceMicros)
	}
}

func TestCostAloneIsStillBillable(t *testing.T) {
	p := newPricer(stubLLMFetcher{in: 1_000_000, out: 1_000_000})

	got, err := p.PriceLLM("ws-1", "m", 0, 0, 250_000)
	if err != nil {
		t.Fatalf("PriceLLM: %v", err)
	}
	if got.CostMicros != 250_000 || got.PriceMicros != 300_000 {
		t.Errorf("got cost=%d price=%d, want 250000/300000 — a cut stream still cost money", got.CostMicros, got.PriceMicros)
	}
}

func TestEmptyEventPricesNothing(t *testing.T) {
	p := newPricer(stubLLMFetcher{in: 1_000_000, out: 1_000_000})

	got, err := p.PriceLLM("ws-1", "m", 0, 0, 0)
	if err != nil {
		t.Fatalf("PriceLLM: %v", err)
	}
	if got.CostMicros != 0 || got.PriceMicros != 0 {
		t.Errorf("got %+v, want a zero result", got)
	}
}
