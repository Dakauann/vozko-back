package analytics_usecase

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	analytics_domain "vozko/domain/analytics"
)

type mockAnalyticsRepo struct {
	profitResult *analytics_domain.ProfitReport
	profitErr    error
	callResult   *analytics_domain.CallAnalyticsReport
	callErr      error

	capturedProfitInput *analytics_domain.ProfitReportInput
	capturedCallInput   *analytics_domain.CallAnalyticsInput
}

func (m *mockAnalyticsRepo) GetProfitReport(input analytics_domain.ProfitReportInput) (*analytics_domain.ProfitReport, error) {
	m.capturedProfitInput = &input
	return m.profitResult, m.profitErr
}

func (m *mockAnalyticsRepo) GetCallAnalytics(input analytics_domain.CallAnalyticsInput) (*analytics_domain.CallAnalyticsReport, error) {
	m.capturedCallInput = &input
	return m.callResult, m.callErr
}

func (m *mockAnalyticsRepo) GetAdminOverview(input analytics_domain.AdminOverviewInput) (*analytics_domain.AdminOverview, error) {
	return nil, nil
}

func (m *mockAnalyticsRepo) GetPlanContractions(input analytics_domain.PlanContractionsInput) (*analytics_domain.PlanContractionsReport, error) {
	return nil, nil
}

func TestGetProfitReportUseCase_Execute_Success(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	wsID := "ws-123"

	expected := &analytics_domain.ProfitReport{
		Period: analytics_domain.Period{StartDate: start, EndDate: end},
		Totals: analytics_domain.FinancialTotals{
			RevenueMicros: 10_000_000,
			CostMicros:    6_000_000,
			ProfitMicros:  4_000_000,
			MarginPct:     40.0,
			TxCount:       5,
		},
		ByService:  []analytics_domain.ServiceBreakdown{},
		TimeSeries: []analytics_domain.PeriodBucket{},
	}

	repo := &mockAnalyticsRepo{profitResult: expected}
	uc := NewGetProfitReportUseCase(repo)

	input := analytics_domain.ProfitReportInput{
		WorkspaceID: &wsID,
		StartDate:   start,
		EndDate:     end,
		Granularity: analytics_domain.GranularityDay,
		TopN:        10,
	}

	result, err := uc.Execute(input)
	require.NoError(t, err)
	assert.Equal(t, expected, result)

	require.NotNil(t, repo.capturedProfitInput)
	assert.Equal(t, &wsID, repo.capturedProfitInput.WorkspaceID)
	assert.Equal(t, analytics_domain.GranularityDay, repo.capturedProfitInput.Granularity)
	assert.Equal(t, 10, repo.capturedProfitInput.TopN)
}

func TestGetProfitReportUseCase_Execute_PropagatesFilters(t *testing.T) {
	planID := "plan-abc"
	cycle := "annual"
	status := "active"
	repo := &mockAnalyticsRepo{profitResult: &analytics_domain.ProfitReport{}}
	uc := NewGetProfitReportUseCase(repo)

	_, err := uc.Execute(analytics_domain.ProfitReportInput{
		StartDate:          time.Now().AddDate(0, -1, 0),
		EndDate:            time.Now(),
		ServiceTypes:       []string{"whatsapp_campaign", "ai"},
		PlanDefinitionID:   &planID,
		BillingCycle:       &cycle,
		SubscriptionStatus: &status,
	})
	require.NoError(t, err)

	require.NotNil(t, repo.capturedProfitInput)
	assert.Equal(t, []string{"whatsapp_campaign", "ai"}, repo.capturedProfitInput.ServiceTypes)
	assert.Equal(t, &planID, repo.capturedProfitInput.PlanDefinitionID)
	assert.Equal(t, &cycle, repo.capturedProfitInput.BillingCycle)
	assert.Equal(t, &status, repo.capturedProfitInput.SubscriptionStatus)
}

func TestGetCallAnalyticsUseCase_Execute_PropagatesPlanScope(t *testing.T) {
	planID := "plan-xyz"
	repo := &mockAnalyticsRepo{callResult: &analytics_domain.CallAnalyticsReport{}}
	uc := NewGetCallAnalyticsUseCase(repo)

	_, err := uc.Execute(analytics_domain.CallAnalyticsInput{
		StartDate:        time.Now().AddDate(0, -1, 0),
		EndDate:          time.Now(),
		PlanDefinitionID: &planID,
	})
	require.NoError(t, err)
	require.NotNil(t, repo.capturedCallInput)
	assert.Equal(t, &planID, repo.capturedCallInput.PlanDefinitionID)
}

func TestGetProfitReportUseCase_Execute_RepoError(t *testing.T) {
	repoErr := errors.New("database unavailable")
	repo := &mockAnalyticsRepo{profitErr: repoErr}
	uc := NewGetProfitReportUseCase(repo)

	_, err := uc.Execute(analytics_domain.ProfitReportInput{
		StartDate: time.Now().AddDate(0, -1, 0),
		EndDate:   time.Now(),
	})

	require.Error(t, err)
	assert.Equal(t, repoErr, err)
}

func TestGetProfitReportUseCase_Execute_PlatformWide(t *testing.T) {
	repo := &mockAnalyticsRepo{
		profitResult: &analytics_domain.ProfitReport{
			TopWorkspaces: []analytics_domain.WorkspaceBreakdown{
				{WorkspaceID: "ws-1", Totals: analytics_domain.FinancialTotals{ProfitMicros: 5_000_000}},
				{WorkspaceID: "ws-2", Totals: analytics_domain.FinancialTotals{ProfitMicros: 3_000_000}},
			},
		},
	}
	uc := NewGetProfitReportUseCase(repo)

	input := analytics_domain.ProfitReportInput{
		WorkspaceID: nil,
		StartDate:   time.Now().AddDate(0, -1, 0),
		EndDate:     time.Now(),
		Granularity: analytics_domain.GranularityMonth,
		TopN:        20,
	}

	result, err := uc.Execute(input)
	require.NoError(t, err)
	assert.Len(t, result.TopWorkspaces, 2)
	assert.Nil(t, repo.capturedProfitInput.WorkspaceID)
}

func TestGetCallAnalyticsUseCase_Execute_Success(t *testing.T) {
	start := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 31, 23, 59, 59, 0, time.UTC)
	wsID := "ws-456"
	agentID := "agent-789"

	expected := &analytics_domain.CallAnalyticsReport{
		Period: analytics_domain.Period{StartDate: start, EndDate: end},
		Financials: analytics_domain.CallDimensionBreakdown{
			Total: analytics_domain.CallDimensionTotals{
				RevenueMicros: 8_000_000,
				CostMicros:    4_000_000,
				ProfitMicros:  4_000_000,
			},
		},
		Usage: analytics_domain.CallUsageTotals{
			TotalCalls:       20,
			TotalDurationSec: 3600,
			BySIPCount:       15,
			ByWebSocketCount: 5,
		},
	}

	repo := &mockAnalyticsRepo{callResult: expected}
	uc := NewGetCallAnalyticsUseCase(repo)

	input := analytics_domain.CallAnalyticsInput{
		WorkspaceID: &wsID,
		AgentID:     &agentID,
		StartDate:   start,
		EndDate:     end,
		Granularity: analytics_domain.GranularityWeek,
	}

	result, err := uc.Execute(input)
	require.NoError(t, err)
	assert.Equal(t, expected, result)

	require.NotNil(t, repo.capturedCallInput)
	assert.Equal(t, &wsID, repo.capturedCallInput.WorkspaceID)
	assert.Equal(t, &agentID, repo.capturedCallInput.AgentID)
	assert.Equal(t, analytics_domain.GranularityWeek, repo.capturedCallInput.Granularity)
}

func TestGetCallAnalyticsUseCase_Execute_RepoError(t *testing.T) {
	repoErr := errors.New("query timeout")
	repo := &mockAnalyticsRepo{callErr: repoErr}
	uc := NewGetCallAnalyticsUseCase(repo)

	_, err := uc.Execute(analytics_domain.CallAnalyticsInput{
		StartDate: time.Now().AddDate(0, -1, 0),
		EndDate:   time.Now(),
	})

	require.Error(t, err)
	assert.Equal(t, repoErr, err)
}

func TestGetCallAnalyticsUseCase_Execute_WithCallSourceFilter(t *testing.T) {
	callSource := "sip"
	repo := &mockAnalyticsRepo{
		callResult: &analytics_domain.CallAnalyticsReport{
			Usage: analytics_domain.CallUsageTotals{
				TotalCalls: 10,
				BySIPCount: 10,
			},
		},
	}
	uc := NewGetCallAnalyticsUseCase(repo)

	input := analytics_domain.CallAnalyticsInput{
		CallSource:  &callSource,
		StartDate:   time.Now().AddDate(0, -1, 0),
		EndDate:     time.Now(),
		Granularity: analytics_domain.GranularityTotal,
	}

	result, err := uc.Execute(input)
	require.NoError(t, err)
	assert.Equal(t, int64(10), result.Usage.TotalCalls)
	assert.Equal(t, &callSource, repo.capturedCallInput.CallSource)
}

func TestGetProfitReport_BRLFieldsPreserved(t *testing.T) {
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 30, 23, 59, 59, 0, time.UTC)

	expected := &analytics_domain.ProfitReport{
		Period: analytics_domain.Period{StartDate: start, EndDate: end},
		Totals: analytics_domain.FinancialTotals{
			RevenueMicros:    10_000_000,
			CostMicros:       6_000_000,
			ProfitMicros:     4_000_000,
			RevenueBRLMicros: 60_000_000,
			CostBRLMicros:    36_000_000,
			ProfitBRLMicros:  24_000_000,
			MarginPct:        40.0,
			TxCount:          3,
		},
		ByService: []analytics_domain.ServiceBreakdown{
			{
				ServiceType: "ai",
				Totals: analytics_domain.FinancialTotals{
					RevenueMicros:    10_000_000,
					CostMicros:       6_000_000,
					ProfitMicros:     4_000_000,
					RevenueBRLMicros: 60_000_000,
					CostBRLMicros:    36_000_000,
					ProfitBRLMicros:  24_000_000,
					MarginPct:        40.0,
					TxCount:          3,
				},
			},
		},
		TimeSeries: []analytics_domain.PeriodBucket{},
	}

	repo := &mockAnalyticsRepo{profitResult: expected}
	uc := NewGetProfitReportUseCase(repo)

	result, err := uc.Execute(analytics_domain.ProfitReportInput{
		StartDate:   start,
		EndDate:     end,
		Granularity: analytics_domain.GranularityTotal,
	})
	require.NoError(t, err)

	assert.Equal(t, int64(60_000_000), result.Totals.RevenueBRLMicros)
	assert.Equal(t, int64(36_000_000), result.Totals.CostBRLMicros)
	assert.Equal(t, int64(24_000_000), result.Totals.ProfitBRLMicros)

	require.Len(t, result.ByService, 1)
	assert.Equal(t, int64(60_000_000), result.ByService[0].Totals.RevenueBRLMicros)
	assert.Equal(t, int64(36_000_000), result.ByService[0].Totals.CostBRLMicros)
	assert.Equal(t, int64(24_000_000), result.ByService[0].Totals.ProfitBRLMicros)
}

func TestGetAdminOverview_BRLCreditsPreserved(t *testing.T) {
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 30, 23, 59, 59, 0, time.UTC)

	expected := &analytics_domain.AdminOverview{
		Period:                      analytics_domain.Period{StartDate: start, EndDate: end},
		TotalPeriodCreditsMicros:    50_000_000,
		TotalPeriodCreditsBRLMicros: 300_000_000,
		RecentSpendings: []analytics_domain.RecentSpending{
			{
				TransactionID:      "tx-1",
				AmountMicros:       1_000_000,
				ExchangeRateMicros: 6_000_000,
			},
		},
		WorkspaceSnapshots: nil,
	}

	repo := &mockAnalyticsRepo{}

	repo.profitResult = nil
	overviewRepo := &mockAnalyticsOverviewRepo{result: expected}
	uc := NewGetAdminOverviewUseCase(overviewRepo)

	result, err := uc.Execute(analytics_domain.AdminOverviewInput{
		StartDate: start,
		EndDate:   end,
	})
	require.NoError(t, err)

	assert.Equal(t, int64(300_000_000), result.TotalPeriodCreditsBRLMicros)
	require.Len(t, result.RecentSpendings, 1)
	assert.Equal(t, int64(6_000_000), result.RecentSpendings[0].ExchangeRateMicros)
}

type mockAnalyticsOverviewRepo struct {
	result *analytics_domain.AdminOverview
}

func (m *mockAnalyticsOverviewRepo) GetProfitReport(input analytics_domain.ProfitReportInput) (*analytics_domain.ProfitReport, error) {
	return nil, nil
}

func (m *mockAnalyticsOverviewRepo) GetCallAnalytics(input analytics_domain.CallAnalyticsInput) (*analytics_domain.CallAnalyticsReport, error) {
	return nil, nil
}

func (m *mockAnalyticsOverviewRepo) GetAdminOverview(input analytics_domain.AdminOverviewInput) (*analytics_domain.AdminOverview, error) {
	return m.result, nil
}

func (m *mockAnalyticsOverviewRepo) GetPlanContractions(input analytics_domain.PlanContractionsInput) (*analytics_domain.PlanContractionsReport, error) {
	return nil, nil
}
