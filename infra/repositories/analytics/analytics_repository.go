package analytics_repository

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	analytics_domain "vozko/domain/analytics"
	"vozko/domain/config"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) analytics_domain.Repository {
	return &repository{db: db}
}

type profitServiceRow struct {
	ServiceType      string `gorm:"column:service_type"`
	RevenueMicros    int64  `gorm:"column:revenue_micros"`
	CostMicros       int64  `gorm:"column:cost_micros"`
	ProfitMicros     int64  `gorm:"column:profit_micros"`
	RevenueBRLMicros int64  `gorm:"column:revenue_brl_micros"`
	CostBRLMicros    int64  `gorm:"column:cost_brl_micros"`
	ProfitBRLMicros  int64  `gorm:"column:profit_brl_micros"`
	TxCount          int64  `gorm:"column:tx_count"`
}

type profitTimeRow struct {
	Period           time.Time `gorm:"column:period"`
	ServiceType      string    `gorm:"column:service_type"`
	RevenueMicros    int64     `gorm:"column:revenue_micros"`
	CostMicros       int64     `gorm:"column:cost_micros"`
	ProfitMicros     int64     `gorm:"column:profit_micros"`
	RevenueBRLMicros int64     `gorm:"column:revenue_brl_micros"`
	CostBRLMicros    int64     `gorm:"column:cost_brl_micros"`
	ProfitBRLMicros  int64     `gorm:"column:profit_brl_micros"`
	TxCount          int64     `gorm:"column:tx_count"`
}

type profitWorkspaceRow struct {
	WorkspaceID      string `gorm:"column:workspace_id"`
	RevenueMicros    int64  `gorm:"column:revenue_micros"`
	CostMicros       int64  `gorm:"column:cost_micros"`
	ProfitMicros     int64  `gorm:"column:profit_micros"`
	RevenueBRLMicros int64  `gorm:"column:revenue_brl_micros"`
	CostBRLMicros    int64  `gorm:"column:cost_brl_micros"`
	ProfitBRLMicros  int64  `gorm:"column:profit_brl_micros"`
	TxCount          int64  `gorm:"column:tx_count"`
}

func (r *repository) profitBase(input analytics_domain.ProfitReportInput) *gorm.DB {
	q := r.db.Table("balance_transactions").
		Where("(type = 'debit' AND is_refund = false) OR (type = 'credit' AND is_refund = true)").
		Where("created_at >= ? AND created_at <= ?", input.StartDate, input.EndDate)
	if input.WorkspaceID != nil {
		q = q.Where("workspace_id = ?", *input.WorkspaceID)
	}
	if len(input.ServiceTypes) > 0 {
		q = q.Where("service_type IN ?", input.ServiceTypes)
	}
	if clause, args := subscriptionScopeClause("workspace_id", input.PlanDefinitionID, input.BillingCycle, input.SubscriptionStatus); clause != "" {
		q = q.Where(clause, args...)
	}
	return q
}

const profitSelectCols = `SUM(CASE WHEN type = 'debit' THEN amount ELSE -amount END) as revenue_micros,
	SUM(CASE WHEN type = 'debit' THEN cost_micros ELSE -cost_micros END) as cost_micros,
	SUM(profit_micros) as profit_micros,
	SUM(CASE WHEN type = 'debit' THEN amount * exchange_rate_micros / 1000000 ELSE -amount * exchange_rate_micros / 1000000 END) as revenue_brl_micros,
	SUM(CASE WHEN type = 'debit' THEN cost_micros * exchange_rate_micros / 1000000 ELSE -cost_micros * exchange_rate_micros / 1000000 END) as cost_brl_micros,
	SUM(profit_micros * exchange_rate_micros / 1000000) as profit_brl_micros,
	COUNT(CASE WHEN type = 'debit' THEN 1 END) as tx_count`

func computeMargin(revenue, profit int64) float64 {
	if revenue == 0 {
		return 0
	}
	return float64(profit) / float64(revenue) * 100
}

func dateTruncUnit(granularity analytics_domain.Granularity) string {
	switch granularity {
	case analytics_domain.GranularityHour:
		return "hour"
	case analytics_domain.GranularityWeek:
		return "week"
	case analytics_domain.GranularityMonth:
		return "month"
	default:
		return "day"
	}
}

// truncBucket truncates a timestamptz column to unit in the system's business
// timezone (config.BrazilianTimezone, the single source of truth) and returns the
// bucket start as the true instant of that local boundary (Brazil midnight for a day),
// so a client formatting it in local time lands on the correct day.
//
// The DB session runs in UTC, so a plain DATE_TRUNC('day', ts) would bucket by UTC
// calendar day and, because Brazil is UTC-3, shift every point in a daily series one
// day at the boundary (a UTC-midnight bucket renders as the previous day locally).
//
// column is a trusted literal (never user input); tz is an IANA name Postgres resolves.
func truncBucket(unit, column string) string {
	tz := config.BrazilianTimezone.String()
	return fmt.Sprintf("(DATE_TRUNC('%s', %s AT TIME ZONE '%s') AT TIME ZONE '%s')", unit, column, tz, tz)
}

func bucketEnd(start time.Time, granularity analytics_domain.Granularity) time.Time {
	switch granularity {
	case analytics_domain.GranularityHour:
		return start.Add(time.Hour)
	case analytics_domain.GranularityWeek:
		return start.AddDate(0, 0, 7)
	case analytics_domain.GranularityMonth:
		return start.AddDate(0, 1, 0)
	default:
		return start.AddDate(0, 0, 1)
	}
}

func (r *repository) GetProfitReport(input analytics_domain.ProfitReportInput) (*analytics_domain.ProfitReport, error) {
	topN := input.TopN
	if topN <= 0 {
		topN = 20
	}

	var serviceRows []profitServiceRow
	err := r.profitBase(input).
		Select("service_type, " + profitSelectCols).
		Group("service_type").
		Order("revenue_micros DESC").
		Scan(&serviceRows).Error
	if err != nil {
		return nil, fmt.Errorf("analytics profit service totals: %w", err)
	}

	var totals analytics_domain.FinancialTotals
	for _, row := range serviceRows {
		totals.RevenueMicros += row.RevenueMicros
		totals.CostMicros += row.CostMicros
		totals.ProfitMicros += row.ProfitMicros
		totals.RevenueBRLMicros += row.RevenueBRLMicros
		totals.CostBRLMicros += row.CostBRLMicros
		totals.ProfitBRLMicros += row.ProfitBRLMicros
		totals.TxCount += row.TxCount
	}
	totals.MarginPct = computeMargin(totals.RevenueMicros, totals.ProfitMicros)

	var timeSeries []analytics_domain.PeriodBucket
	if input.Granularity != analytics_domain.GranularityTotal {
		unit := dateTruncUnit(input.Granularity)
		truncExpr := truncBucket(unit, "created_at")
		var tsRows []profitTimeRow
		err = r.profitBase(input).
			Select(fmt.Sprintf("%s as period, service_type, %s", truncExpr, profitSelectCols)).
			Group(fmt.Sprintf("%s, service_type", truncExpr)).
			Order("period ASC").
			Scan(&tsRows).Error
		if err != nil {
			return nil, fmt.Errorf("analytics profit time series: %w", err)
		}
		timeSeries = buildPeriodBuckets(tsRows, input.Granularity)
	}

	var topWorkspaces []analytics_domain.WorkspaceBreakdown
	if input.WorkspaceID == nil {
		var wsRows []profitWorkspaceRow
		err = r.profitBase(input).
			Select("workspace_id, " + profitSelectCols).
			Group("workspace_id").
			Order("profit_micros DESC").
			Limit(topN).
			Scan(&wsRows).Error
		if err != nil {
			return nil, fmt.Errorf("analytics profit top workspaces: %w", err)
		}
		for _, row := range wsRows {
			topWorkspaces = append(topWorkspaces, analytics_domain.WorkspaceBreakdown{
				WorkspaceID: row.WorkspaceID,
				Totals: analytics_domain.FinancialTotals{
					RevenueMicros:    row.RevenueMicros,
					CostMicros:       row.CostMicros,
					ProfitMicros:     row.ProfitMicros,
					RevenueBRLMicros: row.RevenueBRLMicros,
					CostBRLMicros:    row.CostBRLMicros,
					ProfitBRLMicros:  row.ProfitBRLMicros,
					MarginPct:        computeMargin(row.RevenueMicros, row.ProfitMicros),
					TxCount:          row.TxCount,
				},
			})
		}
	}

	byService := make([]analytics_domain.ServiceBreakdown, len(serviceRows))
	for i, row := range serviceRows {
		byService[i] = analytics_domain.ServiceBreakdown{
			ServiceType: row.ServiceType,
			Totals: analytics_domain.FinancialTotals{
				RevenueMicros:    row.RevenueMicros,
				CostMicros:       row.CostMicros,
				ProfitMicros:     row.ProfitMicros,
				RevenueBRLMicros: row.RevenueBRLMicros,
				CostBRLMicros:    row.CostBRLMicros,
				ProfitBRLMicros:  row.ProfitBRLMicros,
				MarginPct:        computeMargin(row.RevenueMicros, row.ProfitMicros),
				TxCount:          row.TxCount,
			},
		}
	}

	return &analytics_domain.ProfitReport{
		Period:        analytics_domain.Period{StartDate: input.StartDate, EndDate: input.EndDate},
		Totals:        totals,
		ByService:     byService,
		TimeSeries:    timeSeries,
		TopWorkspaces: topWorkspaces,
	}, nil
}

func buildPeriodBuckets(rows []profitTimeRow, granularity analytics_domain.Granularity) []analytics_domain.PeriodBucket {
	type entry struct {
		totals analytics_domain.FinancialTotals
		svcMap map[string]*analytics_domain.FinancialTotals
	}
	buckets := make(map[time.Time]*entry)
	order := make([]time.Time, 0)

	for _, row := range rows {
		e, ok := buckets[row.Period]
		if !ok {
			e = &entry{svcMap: make(map[string]*analytics_domain.FinancialTotals)}
			buckets[row.Period] = e
			order = append(order, row.Period)
		}
		e.totals.RevenueMicros += row.RevenueMicros
		e.totals.CostMicros += row.CostMicros
		e.totals.ProfitMicros += row.ProfitMicros
		e.totals.RevenueBRLMicros += row.RevenueBRLMicros
		e.totals.CostBRLMicros += row.CostBRLMicros
		e.totals.ProfitBRLMicros += row.ProfitBRLMicros
		e.totals.TxCount += row.TxCount
		svc := e.svcMap[row.ServiceType]
		if svc == nil {
			svc = &analytics_domain.FinancialTotals{}
			e.svcMap[row.ServiceType] = svc
		}
		svc.RevenueMicros += row.RevenueMicros
		svc.CostMicros += row.CostMicros
		svc.ProfitMicros += row.ProfitMicros
		svc.RevenueBRLMicros += row.RevenueBRLMicros
		svc.CostBRLMicros += row.CostBRLMicros
		svc.ProfitBRLMicros += row.ProfitBRLMicros
		svc.TxCount += row.TxCount
	}

	result := make([]analytics_domain.PeriodBucket, 0, len(order))
	for _, t := range order {
		e := buckets[t]
		e.totals.MarginPct = computeMargin(e.totals.RevenueMicros, e.totals.ProfitMicros)
		bySvc := make([]analytics_domain.ServiceBreakdown, 0, len(e.svcMap))
		for svcType, ft := range e.svcMap {
			ft.MarginPct = computeMargin(ft.RevenueMicros, ft.ProfitMicros)
			bySvc = append(bySvc, analytics_domain.ServiceBreakdown{ServiceType: svcType, Totals: *ft})
		}
		result = append(result, analytics_domain.PeriodBucket{
			PeriodStart: t,
			PeriodEnd:   bucketEnd(t, granularity),
			Totals:      e.totals,
			ByService:   bySvc,
		})
	}
	return result
}

type callTotalsRow struct {
	TelephonyRevenueMicros int64 `gorm:"column:telephony_revenue_micros"`
	TelephonyCostMicros    int64 `gorm:"column:telephony_cost_micros"`
	TelephonyProfitMicros  int64 `gorm:"column:telephony_profit_micros"`
	TotalRevenueMicros     int64 `gorm:"column:total_revenue_micros"`
	TotalCostMicros        int64 `gorm:"column:total_cost_micros"`
	TotalProfitMicros      int64 `gorm:"column:total_profit_micros"`
	TotalCalls             int64 `gorm:"column:total_calls"`
	TotalDurationSec       int64 `gorm:"column:total_duration_sec"`
	BySIPCount             int64 `gorm:"column:by_sip_count"`
	ByWebSocketCount       int64 `gorm:"column:by_web_socket_count"`
}

type callTimeBucketRow struct {
	Period time.Time `gorm:"column:period"`
	callTotalsRow
}

type callAgentRow struct {
	AgentID string `gorm:"column:agent_id"`
	callTotalsRow
}

// callSelectCols sums what call_billing_records actually stores.
//
// It used to sum stt_/tts_/llm_ columns as well. Those columns do not exist —
// the table carries telephony and total only — so EVERY call analytics query
// failed with `column "stt_revenue_micros" does not exist`. They were the
// residue of a voice-AI cost model the product does not have: AI runs on
// messaging channels, never on a call.
const callSelectCols = `COALESCE(SUM(telephony_revenue_micros), 0) as telephony_revenue_micros,
	COALESCE(SUM(telephony_cost_micros), 0) as telephony_cost_micros,
	COALESCE(SUM(telephony_profit_micros), 0) as telephony_profit_micros,
	COALESCE(SUM(total_revenue_micros), 0) as total_revenue_micros,
	COALESCE(SUM(total_cost_micros), 0) as total_cost_micros,
	COALESCE(SUM(total_profit_micros), 0) as total_profit_micros,
	COUNT(*) as total_calls,
	COALESCE(SUM(duration_sec), 0) as total_duration_sec,
	COUNT(CASE WHEN call_source = 'sip' THEN 1 END) as by_sip_count,
	COUNT(CASE WHEN call_source = 'websocket' THEN 1 END) as by_web_socket_count`

func (r *repository) callBase(input analytics_domain.CallAnalyticsInput) *gorm.DB {
	q := r.db.Table("call_billing_records").
		Where("deleted_at IS NULL").
		Where("status = ?", "charged").
		Where("call_start >= ? AND call_start <= ?", input.StartDate, input.EndDate)
	if input.WorkspaceID != nil {
		q = q.Where("workspace_id = ?", *input.WorkspaceID)
	}
	if input.AgentID != nil {
		q = q.Where("agent_id = ?", *input.AgentID)
	}
	if input.CampaignID != nil {
		q = q.Where("campaign_id = ?", *input.CampaignID)
	}
	if input.CallSource != nil {
		q = q.Where("call_source = ?", *input.CallSource)
	}
	if clause, args := subscriptionScopeClause("workspace_id", input.PlanDefinitionID, input.BillingCycle, input.SubscriptionStatus); clause != "" {
		q = q.Where(clause, args...)
	}
	return q
}

func toCallDimensions(row callTotalsRow) analytics_domain.CallDimensionBreakdown {
	return analytics_domain.CallDimensionBreakdown{
		Telephony: analytics_domain.CallDimensionTotals{RevenueMicros: row.TelephonyRevenueMicros, CostMicros: row.TelephonyCostMicros, ProfitMicros: row.TelephonyProfitMicros},
		Total:     analytics_domain.CallDimensionTotals{RevenueMicros: row.TotalRevenueMicros, CostMicros: row.TotalCostMicros, ProfitMicros: row.TotalProfitMicros},
	}
}

func toCallUsage(row callTotalsRow) analytics_domain.CallUsageTotals {
	return analytics_domain.CallUsageTotals{
		TotalCalls:       row.TotalCalls,
		TotalDurationSec: row.TotalDurationSec,
		BySIPCount:       row.BySIPCount,
		ByWebSocketCount: row.ByWebSocketCount,
	}
}

func (r *repository) GetCallAnalytics(input analytics_domain.CallAnalyticsInput) (*analytics_domain.CallAnalyticsReport, error) {

	var totalsRow callTotalsRow
	err := r.callBase(input).
		Select(callSelectCols).
		Scan(&totalsRow).Error
	if err != nil {
		return nil, fmt.Errorf("analytics call totals: %w", err)
	}

	var timeSeries []analytics_domain.CallPeriodBucket
	if input.Granularity != analytics_domain.GranularityTotal {
		unit := dateTruncUnit(input.Granularity)
		truncExpr := truncBucket(unit, "call_start")
		var tsRows []callTimeBucketRow
		err = r.callBase(input).
			Select(fmt.Sprintf("%s as period, %s", truncExpr, callSelectCols)).
			Group(truncExpr).
			Order("period ASC").
			Scan(&tsRows).Error
		if err != nil {
			return nil, fmt.Errorf("analytics call time series: %w", err)
		}
		timeSeries = make([]analytics_domain.CallPeriodBucket, len(tsRows))
		for i, row := range tsRows {
			timeSeries[i] = analytics_domain.CallPeriodBucket{
				PeriodStart: row.Period,
				PeriodEnd:   bucketEnd(row.Period, input.Granularity),
				Financials:  toCallDimensions(row.callTotalsRow),
				Usage:       toCallUsage(row.callTotalsRow),
			}
		}
	}

	var byAgent []analytics_domain.AgentBreakdown
	if input.AgentID == nil {
		var agentRows []callAgentRow
		err = r.callBase(input).
			Select(fmt.Sprintf("agent_id, %s", callSelectCols)).
			Where("agent_id IS NOT NULL").
			Group("agent_id").
			Order("total_revenue_micros DESC").
			Limit(50).
			Scan(&agentRows).Error
		if err != nil {
			return nil, fmt.Errorf("analytics call agent breakdown: %w", err)
		}
		for _, row := range agentRows {
			if row.AgentID == "" {
				continue
			}
			byAgent = append(byAgent, analytics_domain.AgentBreakdown{
				AgentID:    row.AgentID,
				Financials: toCallDimensions(row.callTotalsRow),
				Usage:      toCallUsage(row.callTotalsRow),
			})
		}
	}

	return &analytics_domain.CallAnalyticsReport{
		Period:     analytics_domain.Period{StartDate: input.StartDate, EndDate: input.EndDate},
		Financials: toCallDimensions(totalsRow),
		Usage:      toCallUsage(totalsRow),
		TimeSeries: timeSeries,
		ByAgent:    byAgent,
	}, nil
}
