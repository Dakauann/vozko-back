package analytics_usecase

import (
	"errors"
	"testing"
	"time"

	analytics_domain "vozko/domain/analytics"
	"vozko/domain/billing"
	"vozko/domain/shared"
)

type metaCostRepo struct {
	captured *analytics_domain.MetaServiceMessageCostInput
	result   *analytics_domain.MetaServiceMessageCostReport
	err      error
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
	return repo, NewGetMetaServiceMessageCostUseCase(repo)
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
	uc := NewGetMetaServiceMessageCostUseCase(repo)
	repo.result = nil

	report, err := uc.Execute(analytics_domain.MetaServiceMessageCostInput{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if report == nil {
		t.Fatal("report = nil, want the empty report the repository returned")
	}
}
