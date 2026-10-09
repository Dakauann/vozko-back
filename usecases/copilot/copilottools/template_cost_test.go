package copilottools

import (
	"testing"

	tmpl "vozko/domain/whatsapp/template"
)

func TestTemplateCostFields(t *testing.T) {
	marketing := &tmpl.Template{Category: tmpl.TemplateCategoryMarketing}
	cases := []struct {
		name        string
		costs       tmpl.TemplateCostReader
		balance     fakeBalance
		noBalance   bool
		eligible    int64
		wantCost    string
		wantBalance string
	}{
		{"cost and balance", fakeCosts{}, 2_000_000, false, 40, "US$ 0.34", "US$ 2.00"},
		{"no balance reader hides the cost", fakeCosts{}, 0, true, 1, costUnavailable, ""},
		{"no price reader hides the cost", nil, 2_000_000, false, 1, costUnavailable, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var fields = templateCostFields(tc.costs, tc.balance, "ws-1", marketing, tc.eligible, "finalCost", formatUSD)
			if tc.noBalance {
				fields = templateCostFields(tc.costs, nil, "ws-1", marketing, tc.eligible, "finalCost", formatUSD)
			}
			if fieldValue(fields, "finalCost") != tc.wantCost || fieldValue(fields, "balance") != tc.wantBalance {
				t.Fatalf("fields = %+v", fields)
			}
		})
	}
}
