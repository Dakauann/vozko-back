package analytics_usecase

import (
	"errors"
	"testing"
	"time"

	analytics_domain "vozko/domain/analytics"
	"vozko/domain/billing"
	"vozko/domain/shared"
	"vozko/domain/workspace/workspace_pricing"
)

type metaCostRepo struct {
	captured *analytics_domain.MetaServiceMessageCostInput
	result   *analytics_domain.MetaServiceMessageCostReport
	err      error
	numbers  []*analytics_domain.NumberMetaCost
	unlinked []*analytics_domain.UnlinkedNumber
}

func (m *metaCostRepo) GetProfitReport(analytics_domain.ProfitReportInput) (*analytics_domain.ProfitReport, error) {
	return nil, nil
}

func (m *metaCostRepo) GetCallAnalytics(analytics_domain.CallAnalyticsInput) (*analytics_domain.CallAnalyticsReport, error) {
	return nil, nil
}

func (m *metaCostRepo) GetAdminOverview(analytics_domain.AdminOverviewInput) (*analytics_domain.AdminOverview, error) {
	return nil, nil
}

func (m *metaCostRepo) GetPlanContractions(analytics_domain.PlanContractionsInput) (*analytics_domain.PlanContractionsReport, error) {
	return nil, nil
}

func (m *metaCostRepo) GetMetaServiceMessageCost(input analytics_domain.MetaServiceMessageCostInput) (*analytics_domain.MetaServiceMessageCostReport, error) {
	m.captured = &input
	if m.result == nil && m.err == nil {
		return &analytics_domain.MetaServiceMessageCostReport{}, nil
	}
	return m.result, m.err
}

func newMetaCostUseCase() (*metaCostRepo, analytics_domain.GetMetaServiceMessageCostUseCase) {
	repo := &metaCostRepo{}
	return repo, NewGetMetaServiceMessageCostUseCase(repo, &fakeCatalog{}, &fakePricing{})
}

func TestEmptyPeriodDefaultsToTheCurrentBillingMonth(t *testing.T) {
	repo, uc := newMetaCostUseCase()

	if _, err := uc.Execute(analytics_domain.MetaServiceMessageCostInput{}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	loc := billing.LocationBRT()
	now := time.Now().In(loc)
	wantStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	wantEnd := wantStart.AddDate(0, 1, 0)

	if !repo.captured.StartDate.Equal(wantStart) {
		t.Errorf("StartDate = %v, want the first instant of this month in BRT (%v)", repo.captured.StartDate, wantStart)
	}
	if !repo.captured.EndDate.Equal(wantEnd) {
		t.Errorf("EndDate = %v, want the first instant of next month in BRT (%v)", repo.captured.EndDate, wantEnd)
	}
}

func TestStatedPeriodIsPassedThrough(t *testing.T) {
	repo, uc := newMetaCostUseCase()

	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)

	if _, err := uc.Execute(analytics_domain.MetaServiceMessageCostInput{StartDate: start, EndDate: end}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !repo.captured.StartDate.Equal(start) || !repo.captured.EndDate.Equal(end) {
		t.Errorf("period = %v..%v, want %v..%v", repo.captured.StartDate, repo.captured.EndDate, start, end)
	}
}

func TestReversedPeriodIsSwappedRatherThanReturningNothing(t *testing.T) {
	repo, uc := newMetaCostUseCase()

	start := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	if _, err := uc.Execute(analytics_domain.MetaServiceMessageCostInput{StartDate: start, EndDate: end}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !repo.captured.StartDate.Equal(end) || !repo.captured.EndDate.Equal(start) {
		t.Errorf("period = %v..%v, want it swapped to %v..%v",
			repo.captured.StartDate, repo.captured.EndDate, end, start)
	}
}

func TestUnknownProviderFallsBackToMeta(t *testing.T) {
	repo, uc := newMetaCostUseCase()

	if _, err := uc.Execute(analytics_domain.MetaServiceMessageCostInput{
		Provider: analytics_domain.ServiceMessageProvider("carrier-pigeon"),
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if repo.captured.Provider != analytics_domain.ServiceMessageProviderMeta {
		t.Errorf("Provider = %q, want meta", repo.captured.Provider)
	}
}

func TestExplicitAllProviderSurvives(t *testing.T) {
	repo, uc := newMetaCostUseCase()

	if _, err := uc.Execute(analytics_domain.MetaServiceMessageCostInput{
		Provider: analytics_domain.ServiceMessageProviderAll,
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if repo.captured.Provider != analytics_domain.ServiceMessageProviderAll {
		t.Errorf("Provider = %q, want all to be preserved", repo.captured.Provider)
	}
}

func TestUnknownSortFallsBackToRatio(t *testing.T) {
	repo, uc := newMetaCostUseCase()

	if _, err := uc.Execute(analytics_domain.MetaServiceMessageCostInput{
		SortBy: analytics_domain.MetaServiceMessageCostSortField("workspace_name; DROP TABLE workspaces"),
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if repo.captured.SortBy != analytics_domain.SortMetaServiceMessageCostRatio {
		t.Errorf("SortBy = %q, want ratio", repo.captured.SortBy)
	}
}

func TestKnownSortFieldsSurvive(t *testing.T) {
	for _, field := range []analytics_domain.MetaServiceMessageCostSortField{
		analytics_domain.SortMetaServiceMessageCostRatio,
		analytics_domain.SortMetaServiceMessageCostServiceMessages,
		analytics_domain.SortMetaServiceMessageCostNetBillableSends,
		analytics_domain.SortMetaServiceMessageCostWorkspaceName,
	} {
		repo, uc := newMetaCostUseCase()
		if _, err := uc.Execute(analytics_domain.MetaServiceMessageCostInput{SortBy: field}); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if repo.captured.SortBy != field {
			t.Errorf("SortBy = %q, want %q", repo.captured.SortBy, field)
		}
	}
}

func TestSortOrderDefaultsToDescending(t *testing.T) {
	repo, uc := newMetaCostUseCase()

	if _, err := uc.Execute(analytics_domain.MetaServiceMessageCostInput{}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if repo.captured.SortOrder != shared.SortDesc {
		t.Errorf("SortOrder = %q, want desc", repo.captured.SortOrder)
	}
}

func TestAscendingSortOrderSurvives(t *testing.T) {
	repo, uc := newMetaCostUseCase()

	if _, err := uc.Execute(analytics_domain.MetaServiceMessageCostInput{SortOrder: shared.SortAsc}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if repo.captured.SortOrder != shared.SortAsc {
		t.Errorf("SortOrder = %q, want asc", repo.captured.SortOrder)
	}
}

func TestPaginationIsClamped(t *testing.T) {
	cases := []struct {
		name             string
		page, size       int
		wantPage, wantSz int
	}{
		{"zero becomes the first page and the default size", 0, 0, 1, shared.DefaultPageSize},
		{"a negative page cannot produce a negative offset", -5, 25, 1, 25},
		{"an oversized page is clamped, not honoured", 1, 100_000, 1, shared.MaxPageSize},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, uc := newMetaCostUseCase()
			if _, err := uc.Execute(analytics_domain.MetaServiceMessageCostInput{
				Page: tc.page, PageSize: tc.size,
			}); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if repo.captured.Page != tc.wantPage {
				t.Errorf("Page = %d, want %d", repo.captured.Page, tc.wantPage)
			}
			if repo.captured.PageSize != tc.wantSz {
				t.Errorf("PageSize = %d, want %d", repo.captured.PageSize, tc.wantSz)
			}
		})
	}
}

func TestUseCaseDoesNotAssertTheInferredFlag(t *testing.T) {
	repo, uc := newMetaCostUseCase()
	repo.result = &analytics_domain.MetaServiceMessageCostReport{InferredOnly: false}

	report, err := uc.Execute(analytics_domain.MetaServiceMessageCostInput{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if report.InferredOnly {
		t.Error("the usecase overrode the coverage-derived flag the repository set")
	}
}

func TestRepositoryErrorPropagates(t *testing.T) {
	repo, uc := newMetaCostUseCase()
	repo.err = errors.New("query timed out")

	if _, err := uc.Execute(analytics_domain.MetaServiceMessageCostInput{}); err == nil {
		t.Fatal("Execute() error = nil, want the repository error")
	}
}

func TestNilReportWithNoErrorDoesNotPanic(t *testing.T) {
	repo := &metaCostRepo{result: nil, err: nil}
	uc := NewGetMetaServiceMessageCostUseCase(repo, &fakeCatalog{}, &fakePricing{})
	repo.result = nil

	report, err := uc.Execute(analytics_domain.MetaServiceMessageCostInput{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if report == nil {
		t.Fatal("report = nil, want the empty report the repository returned")
	}
}

func (m *metaCostRepo) MetaCostNumbers(analytics_domain.MetaServiceMessageCostInput) ([]*analytics_domain.NumberMetaCost, error) {
	return m.numbers, nil
}

func (m *metaCostRepo) UnlinkedServiceMessages(analytics_domain.MetaServiceMessageCostInput) ([]*analytics_domain.UnlinkedNumber, error) {
	return m.unlinked, nil
}

func (m *metaCostRepo) InvoiceAccounts(time.Time, time.Time) ([]analytics_domain.InvoiceAccount, error) {
	return nil, nil
}

type fakeCatalog struct {
	items []workspace_pricing.PricingItem
	err   error
}

func (f *fakeCatalog) ListDefaultPricingItems() ([]workspace_pricing.PricingItem, error) {
	return f.items, f.err
}

func TestTheReportIsConvertedWithTheCatalogExchangeRateAndCarriesItsDetails(t *testing.T) {
	repo := &metaCostRepo{
		result: &analytics_domain.MetaServiceMessageCostReport{Totals: analytics_domain.MetaServiceMessageCostTotals{ServiceMessages: 10, MetaConfirmed: 4, PaidMicros: 200_000, ServiceCharges: []analytics_domain.ServiceCharge{{WorkspaceID: "ws-1", MetaPayer: "vozko", Charged: 4}}}},
		numbers:  []*analytics_domain.NumberMetaCost{{PhoneID: "p-1", Answered: 4, Charged: 4}},
		unlinked: []*analytics_domain.UnlinkedNumber{{PhoneNumberID: "885", Messages: 2}},
	}
	catalog := &fakeCatalog{items: []workspace_pricing.PricingItem{
		{Category: workspace_pricing.CategoryExchangeRate, Service: "usd_to_brl", Metric: "per_unit", PriceMicros: 5_000_000},
	}}
	pricing := &fakePricing{costs: map[string]int64{"ws-1": 5_000}}
	report, err := NewGetMetaServiceMessageCostUseCase(repo, catalog, pricing).Execute(analytics_domain.MetaServiceMessageCostInput{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Rates != (analytics_domain.CostRates{USDToBRLMicros: 5_000_000}) {
		t.Fatalf("rates %+v", report.Rates)
	}
	if report.Totals.PaidByClients.BRLMicros == nil || *report.Totals.PaidByClients.BRLMicros != 1_000_000 {
		t.Fatalf("paid %+v", report.Totals.PaidByClients)
	}
	if c := report.Totals.ConfirmedServiceCost; c == nil || c.BRLMicros == nil || *c.BRLMicros != 100_000 {
		t.Fatalf("service %+v", c)
	}
	if report.Numbers[0].State != analytics_domain.NumberCharging || report.Totals.UnlinkedServiceMessages != 2 {
		t.Fatalf("details %+v %+v", report.Numbers[0], report.Totals)
	}
}

func TestAnUnreadableCatalogIsAnErrorNotAZeroCost(t *testing.T) {
	repo := &metaCostRepo{}
	_, err := NewGetMetaServiceMessageCostUseCase(repo, &fakeCatalog{err: errors.New("db down")}, &fakePricing{}).Execute(analytics_domain.MetaServiceMessageCostInput{})
	if err == nil {
		t.Fatal("amounts must not be shown when the exchange rate could not be read")
	}
}

type fakePricing struct {
	costs map[string]int64
	err   error
	calls []string
}

func (f *fakePricing) ResolveForWorkspace(workspaceID string) ([]workspace_pricing.ResolvedPricingItem, error) {
	f.calls = append(f.calls, workspaceID)
	if f.err != nil {
		return nil, f.err
	}
	return []workspace_pricing.ResolvedPricingItem{{Category: workspace_pricing.CategoryWhatsApp, Service: workspace_pricing.WhatsAppServiceServiceMessage, Metric: "per_message", CostMicros: f.costs[workspaceID]}}, nil
}

func TestServiceCostIsResolvedPerChargedWorkspaceLikeBilling(t *testing.T) {
	repo := &metaCostRepo{result: &analytics_domain.MetaServiceMessageCostReport{Totals: analytics_domain.MetaServiceMessageCostTotals{
		ServiceCharges: []analytics_domain.ServiceCharge{{WorkspaceID: "ws-1", MetaPayer: "vozko", Charged: 4}, {WorkspaceID: "ws-2", MetaPayer: "vozko", Charged: 2}},
	}}}
	pricing := &fakePricing{costs: map[string]int64{"ws-1": 5_000}}
	report, err := NewGetMetaServiceMessageCostUseCase(repo, &fakeCatalog{}, pricing).Execute(analytics_domain.MetaServiceMessageCostInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pricing.calls) != 2 || report.Totals.ServiceCostMissing != 1 || report.Totals.ConfirmedServiceCost != nil {
		t.Fatalf("calls %v totals %+v", pricing.calls, report.Totals)
	}
}

func TestAnUnresolvablePriceIsAnErrorNotAZeroCost(t *testing.T) {
	repo := &metaCostRepo{result: &analytics_domain.MetaServiceMessageCostReport{Totals: analytics_domain.MetaServiceMessageCostTotals{
		ServiceCharges: []analytics_domain.ServiceCharge{{WorkspaceID: "ws-1", Charged: 4}},
	}}}
	_, err := NewGetMetaServiceMessageCostUseCase(repo, &fakeCatalog{}, &fakePricing{err: errors.New("db down")}).Execute(analytics_domain.MetaServiceMessageCostInput{})
	if err == nil {
		t.Fatal("a price that could not be read must not become zero")
	}
}
