package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	workspace_addon "vozko/domain/workspace/workspace_addon"
)

func TestToCustomerAddonDefinitions_HidesCost(t *testing.T) {
	defs := []*workspace_addon.AddonDefinition{
		{
			ID:                 "addon-1",
			Key:                "whatsapp_channel",
			Name:               "Canal WhatsApp",
			MonthlyPriceMicros: 25_000_000,
			AnnualPriceMicros:  250_000_000,
			MonthlyCostMicros:  9_990_000,
			AnnualCostMicros:   99_900_000,
			IsActive:           true,
		},
	}

	blob, err := json.Marshal(toCustomerAddonDefinitions(defs))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(blob)

	for _, forbidden := range []string{"monthlyCostMicros", "annualCostMicros", "9990000", "99900000"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("customer addon payload leaks %q:\n%s", forbidden, out)
		}
	}
	for _, required := range []string{"monthlyPriceMicros", "25000000", "\"whatsapp_channel\""} {
		if !strings.Contains(out, required) {
			t.Fatalf("customer addon payload missing %q:\n%s", required, out)
		}
	}
}
