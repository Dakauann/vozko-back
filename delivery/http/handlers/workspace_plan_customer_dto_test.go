package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	workspace_plan "vozko/domain/workspace/workspace_plan"
)

// TestToCustomerPlanDetails_HidesCostAndMarkup guards the customer plan-catalog contract: the end
// customer must see the final PRICE only, never our internal vendor cost or markup. A regression here
// would leak margin to every customer's browser via the public /plans endpoint.
func TestToCustomerPlanDetails_HidesCostAndMarkup(t *testing.T) {
	plan := &workspace_plan.PlanDefinition{
		ID:                "plan-1",
		Name:              "Pro",
		BasePriceBRLCents: 109_900,
		PricingItems: []workspace_plan.PlanPricingItem{
			{
				ID:          "pi-1",
				Category:    "voice",
				Service:     "tts",
				Metric:      "per_minute",
				CostMicros:  1_234_567, // internal cost -> must NOT appear
				PriceMicros: 3_000_000, // final price -> must appear
				MarkupPct:   1.43,      // internal markup -> must NOT appear
				Currency:    "USD",
			},
		},
	}

	blob, err := json.Marshal(toCustomerPlanDetails(plan))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(blob)

	for _, forbidden := range []string{"costMicros", "markupPct", "1234567", "1.43"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("customer plan payload leaks %q:\n%s", forbidden, out)
		}
	}
	// The customer must still get the price and the plan basics.
	for _, required := range []string{"priceMicros", "3000000", "basePriceBRLCents", "\"Pro\""} {
		if !strings.Contains(out, required) {
			t.Fatalf("customer plan payload missing %q:\n%s", required, out)
		}
	}
}
