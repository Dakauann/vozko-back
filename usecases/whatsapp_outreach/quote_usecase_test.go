package whatsapp_outreach

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
)

type quoteTemplates map[string]*template.Template

func (q quoteTemplates) FindByID(id string) (*template.Template, error) {
	if t, ok := q[id]; ok {
		return t, nil
	}
	return nil, errors.New("not found")
}

type quotePrices int64

func (p quotePrices) GetTemplateCostMicros(string, string) (int64, error) { return int64(p), nil }

type quoteBalances struct {
	micros int64
	err    error
}

func (b quoteBalances) GetBalance(string) (int64, error) { return b.micros, b.err }

func TestQuoteUseCase(t *testing.T) {
	templates := quoteTemplates{"t1": {Category: template.TemplateCategoryMarketing}}
	boom := errors.New("redis down")
	cases := []struct {
		name           string
		templateID     string
		price          quotePrices
		balances       balance.BalanceReader
		wantAffordable bool
		wantErr        error
	}{
		{"affordable send", "t1", 60_000, quoteBalances{micros: 60_000}, true, nil},
		{"balance below one send", "t1", 60_000, quoteBalances{micros: 59_999}, false, nil},
		{"unknown template", "nope", 60_000, quoteBalances{micros: 1}, false, wo.ErrTemplateNotFound},
		{"no price", "t1", 0, quoteBalances{micros: 1}, false, template.ErrPricingUnavailable},
		{"unreadable balance is refused, not shown as zero", "t1", 60_000, quoteBalances{err: boom}, false, boom},
		{"no balance reader", "t1", 60_000, nil, false, template.ErrBillingNotConfigured},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := NewQuoteUseCase(templates, &fakeGrant{granted: true}, tc.price, tc.balances)
			q, err := uc.Execute(context.Background(), "ws-1", tc.templateID, "bp-1")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if q.Category != "MARKETING" || q.PriceMicros != 60_000 || q.Affordable != tc.wantAffordable {
				t.Fatalf("unexpected quote %+v", q)
			}
		})
	}
}

func TestQuoteUseCase_RequiresWorkspace(t *testing.T) {
	uc := NewQuoteUseCase(quoteTemplates{}, &fakeGrant{granted: true}, quotePrices(1), quoteBalances{})
	if _, err := uc.Execute(context.Background(), " ", "t1", "bp-1"); !errors.Is(err, template.ErrWorkspaceRequired) {
		t.Fatalf("want ErrWorkspaceRequired, got %v", err)
	}
}

func TestQuoteUseCase_ReadsOnlyATemplateGrantedToTheWorkspace(t *testing.T) {
	templates := quoteTemplates{"t1": {ID: "t1", Category: template.TemplateCategoryMarketing}}
	boom := errors.New("grants down")
	cases := []struct {
		name  string
		grant *fakeGrant
		want  error
	}{
		{"a template of another workspace", &fakeGrant{granted: false}, wo.ErrTemplateForbidden},
		{"unreadable grants", &fakeGrant{err: boom}, boom},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := NewQuoteUseCase(templates, tc.grant, quotePrices(60_000), quoteBalances{micros: 60_000}).Execute(context.Background(), "ws-1", "t1", "bp-1")
			if !errors.Is(err, tc.want) || q != nil {
				t.Fatalf("quote %+v err %v, want %v", q, err, tc.want)
			}
		})
	}
}

func TestQuoteUseCase_RefusesWithoutTheGrantCheck(t *testing.T) {
	templates := quoteTemplates{"t1": {ID: "t1", Category: template.TemplateCategoryMarketing}}
	if _, err := NewQuoteUseCase(templates, nil, quotePrices(60_000), quoteBalances{micros: 60_000}).Execute(context.Background(), "ws-1", "t1", "bp-1"); !errors.Is(err, wo.ErrTemplateForbidden) {
		t.Fatalf("err = %v, want ErrTemplateForbidden", err)
	}
}
