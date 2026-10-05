package analytics_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	analytics_domain "vozko/domain/analytics"
)

type invoiceAccounts struct {
	accounts []analytics_domain.InvoiceAccount
	err      error
}

func (r *invoiceAccounts) InvoiceAccounts(time.Time, time.Time) ([]analytics_domain.InvoiceAccount, error) {
	return r.accounts, r.err
}

type pricingAnalytics struct {
	mu     sync.Mutex
	points map[string][]analytics_domain.MetaVolumePoint
	calls  []string
}

func (p *pricingAnalytics) Volumes(_ context.Context, wabaID, _ string, _, _ time.Time) ([]analytics_domain.MetaVolumePoint, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, wabaID)
	points, ok := p.points[wabaID]
	if !ok {
		return nil, errors.New("permission denied")
	}
	return points, nil
}

func templates(volume int64) []analytics_domain.MetaVolumePoint {
	return []analytics_domain.MetaVolumePoint{
		{Category: "UTILITY", PricingType: "REGULAR", Volume: volume},
		{Category: "SERVICE", PricingType: "FREE_CUSTOMER_SERVICE", Volume: 40},
	}
}

func TestEveryWhatsAppAccountGetsAnAnswerAndOnlyReadableOnesAskMeta(t *testing.T) {
	repo := &invoiceAccounts{accounts: []analytics_domain.InvoiceAccount{
		{WABAID: "ok", Provider: "meta", AccessToken: "t", TemplateSends: 1_000},
		{WABAID: "off", Provider: "meta", AccessToken: "t", TemplateSends: 1_000},
		{WABAID: "denied", Provider: "meta", AccessToken: "t", ServiceMessages: 5},
		{WABAID: "d360", Provider: "dialog360", AccessToken: "t", TemplateSends: 3},
		{WABAID: "notoken", Provider: "meta", TemplateSends: 2},
		{WABAID: "quiet", Provider: "meta", AccessToken: "t"},
	}}
	meta := &pricingAnalytics{points: map[string][]analytics_domain.MetaVolumePoint{
		"ok":  templates(1_010),
		"off": templates(1_500),
	}}
	report, err := NewGetMetaInvoiceCheckUseCase(repo, meta).Execute(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]analytics_domain.WABAInvoiceCheck{}
	for _, a := range report.Accounts {
		states[a.WABAID] = a
	}
	if states["ok"].State != analytics_domain.InvoiceMatched || states["off"].State != analytics_domain.InvoiceToCheck {
		t.Fatalf("states %+v", states)
	}
	if states["denied"].Reason != analytics_domain.ReasonMetaUnreadable || states["d360"].Reason != analytics_domain.ReasonDialog360 || states["notoken"].Reason != analytics_domain.ReasonNoToken {
		t.Fatalf("unavailable %+v", states)
	}
	if len(meta.calls) != 3 {
		t.Fatalf("only active Meta accounts with a token are asked, got %v", meta.calls)
	}
	if report.Accounts[0].WABAID != "off" {
		t.Fatalf("accounts to check come first, got %+v", report.Accounts[0])
	}
	if report.Totals != (analytics_domain.InvoiceTotals{MetaFreeService: 80, UnavailableAccounts: 3, IdleAccounts: 1}) {
		t.Fatalf("totals %+v", report.Totals)
	}
	if report.Period.StartDate.IsZero() || !report.Period.EndDate.After(report.Period.StartDate) {
		t.Fatalf("an empty period becomes the current month, got %+v", report.Period)
	}
}

func TestNoInvoiceCheckWithoutTheAccounts(t *testing.T) {
	_, err := NewGetMetaInvoiceCheckUseCase(&invoiceAccounts{err: errors.New("db down")}, &pricingAnalytics{}).Execute(context.Background(), time.Time{}, time.Time{})
	if err == nil {
		t.Fatal("an unread account list must not look like no accounts")
	}
}

func TestAccountsWithoutActivityAreNeverSentToMeta(t *testing.T) {
	repo := &invoiceAccounts{accounts: []analytics_domain.InvoiceAccount{
		{WABAID: "quiet-1", Provider: "meta", AccessToken: "t"},
		{WABAID: "quiet-2", Provider: "meta", AccessToken: "t"},
	}}
	meta := &pricingAnalytics{}
	report, err := NewGetMetaInvoiceCheckUseCase(repo, meta).Execute(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.calls) != 0 || len(report.Accounts) != 0 || report.Totals.IdleAccounts != 2 {
		t.Fatalf("calls %v accounts %+v totals %+v", meta.calls, report.Accounts, report.Totals)
	}
}
