package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	workspace_plan "vozko/domain/workspace/workspace_plan"
)

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
				CostMicros:  1_234_567,
				PriceMicros: 3_000_000,
				MarkupPct:   1.43,
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
	for _, required := range []string{"priceMicros", "3000000", "basePriceBRLCents", "\"Pro\""} {
		if !strings.Contains(out, required) {
			t.Fatalf("customer plan payload missing %q:\n%s", required, out)
		}
	}
}
