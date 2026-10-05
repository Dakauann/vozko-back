package ai

import "testing"

func TestTheCostOfACallComesFromTheModelsPrices(t *testing.T) {
	m := ModelInfo{PromptPrice: 3, CompletionPrice: 15, ContextLength: 200_000}
	got := m.CostMicros(Usage{PromptTokens: 10_000, CompletionTokens: 1_000})
	if got != 45_000 {
		t.Fatalf("got %d micros, want 45000 (US$ 0.045)", got)
	}
}

func TestOnlyACatalogedModelHasKnownLimits(t *testing.T) {
	cases := map[string]struct {
		m    ModelInfo
		want bool
	}{
		"window and prices":  {ModelInfo{ContextLength: 128_000, PromptPrice: 0.15, CompletionPrice: 0.6}, true},
		"no window":          {ModelInfo{PromptPrice: 0.15, CompletionPrice: 0.6}, false},
		"no prices":          {ModelInfo{ContextLength: 128_000}, false},
		"free prompt tokens": {ModelInfo{ContextLength: 128_000, CompletionPrice: 0.6}, true},
	}
	for name, tc := range cases {
		if got := tc.m.HasKnownLimits(); got != tc.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
}
