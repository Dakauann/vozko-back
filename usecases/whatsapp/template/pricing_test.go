package template_usecase

import (
	"errors"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/whatsapp/template"
)

type quoteCosts struct {
	micros   int64
	err      error
	category string
}

func (c *quoteCosts) GetTemplateCostMicros(_ string, category string) (int64, error) {
	c.category = category
	return c.micros, c.err
}

type quoteBalance struct {
	micros int64
	err    error
}

func (b quoteBalance) GetBalance(string) (int64, error) { return b.micros, b.err }

func TestQuoteSend(t *testing.T) {
	marketing := &template.Template{Category: template.TemplateCategory("marketing")}
	boom := errors.New("db down")
	cases := []struct {
		name     string
		tmpl     *template.Template
		costs    *quoteCosts
		balances balance.BalanceReader
		eligible int64
		wantCost int64
		wantErr  error
	}{
		{"prices by category against the balance", marketing, &quoteCosts{micros: 60_000}, quoteBalance{micros: 1_000_000}, 3, 180_000, nil},
		{"unknown template", nil, &quoteCosts{micros: 60_000}, quoteBalance{}, 1, 0, template.ErrTemplateNotFound},
		{"no category", &template.Template{}, &quoteCosts{micros: 60_000}, quoteBalance{}, 1, 0, template.ErrTemplateCategoryUnavailable},
		{"price lookup fails", marketing, &quoteCosts{err: boom}, quoteBalance{}, 1, 0, boom},
		{"no price configured", marketing, &quoteCosts{}, quoteBalance{}, 1, 0, template.ErrPricingUnavailable},
		{"balance lookup fails", marketing, &quoteCosts{micros: 60_000}, quoteBalance{err: boom}, 1, 0, boom},
		{"no balance reader", marketing, &quoteCosts{micros: 60_000}, nil, 1, 0, template.ErrBillingNotConfigured},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := QuoteSend(tc.costs, tc.balances, "ws-1", tc.tmpl, tc.eligible)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil || q.CostMicros != tc.wantCost || q.Currency == "" || !q.Affordable || q.Category != "MARKETING" {
				t.Fatalf("got %+v, %v", q, err)
			}
			if tc.costs.category != "MARKETING" {
				t.Fatalf("priced as %q", tc.costs.category)
			}
		})
	}
	if _, err := QuoteSend(nil, quoteBalance{}, "ws-1", marketing, 1); !errors.Is(err, template.ErrBillingNotConfigured) {
		t.Fatalf("a missing price reader must refuse, got %v", err)
	}
}

func TestPriceOf(t *testing.T) {
	marketing := &template.Template{Category: template.TemplateCategory("marketing")}
	category, unit, err := PriceOf(&quoteCosts{micros: 60_000}, "ws-1", marketing)
	if err != nil || category != "MARKETING" || unit != 60_000 {
		t.Fatalf("got %q, %d, %v", category, unit, err)
	}
	for name, costs := range map[string]template.TemplateCostReader{
		"no price configured": &quoteCosts{},
		"a negative price":    &quoteCosts{micros: -1},
	} {
		if _, _, err := PriceOf(costs, "ws-1", marketing); !errors.Is(err, template.ErrPricingUnavailable) {
			t.Fatalf("%s: want ErrPricingUnavailable, got %v", name, err)
		}
	}
}
