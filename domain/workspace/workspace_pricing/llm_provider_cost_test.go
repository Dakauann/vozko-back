package workspace_pricing

import "testing"

// Deriving LLM cost from tokens × a price table reproduces an invoice the
// provider already computed, and can only drift from it: OpenRouter also
// charges reasoning tokens, cache writes, image and audio tokens, web search
// and per-request fees, discounts cache reads, and reprices a model the moment
// an upstream does. usage.cost carries all of that. These pin that the reported
// figure wins, and — the part that must never break — that its absence changes
// nothing about the old behaviour.

type stubLLMFetcher struct{ in, out int64 }

func (s stubLLMFetcher) FetchLLMPriceMicros(string) (int64, int64, error) {
	return s.in, s.out, nil
}

// newPricer builds a pricer with NO configured LLM item, so the per-token path
// falls through to the fetcher and the markup to its 20% default — the shape
// production actually runs in.
func newPricer(f LLMPriceFetcher) Pricer {
	return NewPricer(stubPricingRepo{}, WithLLMPriceFetcher(f))
}

func TestProviderCostWinsOverTheTokenEstimate(t *testing.T) {
	// The estimate would be 1M × $1.00 = 1_000_000µ. The provider says the call
	// really cost 1_600_000µ — the deepseek-v4-pro repricing, in miniature.
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

// The margin must be taken on what we PAY, not on what we guessed we paid.
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

// A zero cost is "not reported", never "free". Events already in the queue when
// this ships carry no cost field, and they must price exactly as before.
func TestAbsentProviderCostFallsBackToTokens(t *testing.T) {
	p := newPricer(stubLLMFetcher{in: 1_000_000, out: 2_000_000})

	got, err := p.PriceLLM("ws-1", "m", 1_000_000, 500_000, 0)
	if err != nil {
		t.Fatalf("PriceLLM: %v", err)
	}
	// 1M in @ $1/M + 0.5M out @ $2/M = 2_000_000µ
	if got.CostMicros != 2_000_000 {
		t.Errorf("CostMicros = %d, want the token estimate 2000000", got.CostMicros)
	}
	if got.PriceMicros != 2_400_000 {
		t.Errorf("PriceMicros = %d, want 2400000", got.PriceMicros)
	}
}

// A cut stream can report a cost with no usable token counts. Refusing to bill
// it because the tokens are zero is exactly the leak the recovery path exists
// to close.
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

// Nothing at all is still nothing: no tokens and no cost must not fabricate a charge.
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
