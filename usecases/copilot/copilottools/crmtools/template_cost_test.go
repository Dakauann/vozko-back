package crmtools

import (
	"errors"
	"testing"

	"vozko/domain/balance"
	tmpl "vozko/domain/whatsapp/template"
	"vozko/usecases/copilot/copilottools/tooltest"
)

type walletRate struct {
	rate float64
	err  error
}

func (w walletRate) CurrentRate() (float64, error) { return w.rate, w.err }

func TestTemplateCostFields(t *testing.T) {
	marketing := &tmpl.Template{Category: tmpl.TemplateCategoryMarketing}
	cases := []struct {
		name        string
		costs       tmpl.TemplateCostReader
		balance     balance.BalanceReader
		rates       balance.ExchangeRate
		eligible    int64
		wantCost    string
		wantBalance string
	}{
		{"cost and balance in reais", fakeCosts{}, fakeBalance(2_000_000), walletRate{rate: 5}, 40, "R$ 1,70", "R$ 10,00"},
		{"no wallet rate hides the cost", fakeCosts{}, fakeBalance(2_000_000), walletRate{err: errors.New("no rate")}, 40, costUnavailable, ""},
		{"no rate reader hides the cost", fakeCosts{}, fakeBalance(2_000_000), nil, 40, costUnavailable, ""},
		{"no balance reader hides the cost", fakeCosts{}, nil, walletRate{rate: 5}, 1, costUnavailable, ""},
		{"no price reader hides the cost", nil, fakeBalance(2_000_000), walletRate{rate: 5}, 1, costUnavailable, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := templateCostFields(tc.costs, tc.balance, tc.rates, "ws-1", marketing, tc.eligible, "finalCost")
			if tooltest.FieldValue(fields, "finalCost") != tc.wantCost || tooltest.FieldValue(fields, "balance") != tc.wantBalance {
				t.Fatalf("fields = %+v", fields)
			}
		})
	}
}
