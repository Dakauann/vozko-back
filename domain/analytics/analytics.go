package analytics

import "time"

type Granularity string

const (
	GranularityHour  Granularity = "hour"
	GranularityDay   Granularity = "day"
	GranularityWeek  Granularity = "week"
	GranularityMonth Granularity = "month"

	GranularityTotal Granularity = "total"
)

type Period struct {
	StartDate time.Time `json:"startDate"`
	EndDate   time.Time `json:"endDate"`
}

type FinancialTotals struct {
	RevenueMicros    int64   `json:"revenueMicros"`
	CostMicros       int64   `json:"costMicros"`
	ProfitMicros     int64   `json:"profitMicros"`
	RevenueBRLMicros int64   `json:"revenueBRLMicros"`
	CostBRLMicros    int64   `json:"costBRLMicros"`
	ProfitBRLMicros  int64   `json:"profitBRLMicros"`
	MarginPct        float64 `json:"marginPct"`
	TxCount          int64   `json:"txCount"`
}

type ServiceBreakdown struct {
	ServiceType string          `json:"serviceType"`
	Totals      FinancialTotals `json:"totals"`
}

type WorkspaceBreakdown struct {
	WorkspaceID string          `json:"workspaceId"`
	Totals      FinancialTotals `json:"totals"`
}

type PeriodBucket struct {
	PeriodStart time.Time          `json:"periodStart"`
	PeriodEnd   time.Time          `json:"periodEnd"`
	Totals      FinancialTotals    `json:"totals"`
	ByService   []ServiceBreakdown `json:"byService,omitempty"`
}

type ProfitReport struct {
	Period     Period             `json:"period"`
	Totals     FinancialTotals    `json:"totals"`
	ByService  []ServiceBreakdown `json:"byService"`
	TimeSeries []PeriodBucket     `json:"timeSeries"`

	TopWorkspaces []WorkspaceBreakdown `json:"topWorkspaces,omitempty"`
}

type ProfitReportInput struct {
	WorkspaceID  *string
	ServiceTypes []string
	StartDate    time.Time
	EndDate      time.Time
	Granularity  Granularity

	PlanDefinitionID   *string
	BillingCycle       *string
	SubscriptionStatus *string

	TopN int
}

type CallDimensionTotals struct {
	RevenueMicros int64 `json:"revenueMicros"`
	CostMicros    int64 `json:"costMicros"`
	ProfitMicros  int64 `json:"profitMicros"`
}

type CallDimensionBreakdown struct {
	STT       CallDimensionTotals `json:"stt"`
	TTS       CallDimensionTotals `json:"tts"`
	LLM       CallDimensionTotals `json:"llm"`
	Telephony CallDimensionTotals `json:"telephony"`
	Total     CallDimensionTotals `json:"total"`
}

type CallUsageTotals struct {
	TotalCalls               int64   `json:"totalCalls"`
	TotalDurationSec         int64   `json:"totalDurationSec"`
	TotalSTTAudioSec         float64 `json:"totalSTTAudioSec"`
	TotalTTSChars            int64   `json:"totalTTSChars"`
	TotalLLMPromptTokens     int64   `json:"totalLLMPromptTokens"`
	TotalLLMCompletionTokens int64   `json:"totalLLMCompletionTokens"`
	BySIPCount               int64   `json:"bySIPCount"`
	ByWebSocketCount         int64   `json:"byWebSocketCount"`
}

type AgentBreakdown struct {
	AgentID    string                 `json:"agentId"`
	Financials CallDimensionBreakdown `json:"financials"`
	Usage      CallUsageTotals        `json:"usage"`
}

type CallPeriodBucket struct {
	PeriodStart time.Time              `json:"periodStart"`
	PeriodEnd   time.Time              `json:"periodEnd"`
	Financials  CallDimensionBreakdown `json:"financials"`
	Usage       CallUsageTotals        `json:"usage"`
}

type CallAnalyticsReport struct {
	Period     Period                 `json:"period"`
	Financials CallDimensionBreakdown `json:"financials"`
	Usage      CallUsageTotals        `json:"usage"`
	TimeSeries []CallPeriodBucket     `json:"timeSeries"`

	ByAgent []AgentBreakdown `json:"byAgent,omitempty"`
}

type CallAnalyticsInput struct {
	WorkspaceID *string
	AgentID     *string
	CampaignID  *string
	CallSource  *string
	StartDate   time.Time
	EndDate     time.Time
	Granularity Granularity

	PlanDefinitionID   *string
	BillingCycle       *string
	SubscriptionStatus *string
}

type Repository interface {
	GetProfitReport(input ProfitReportInput) (*ProfitReport, error)
	GetCallAnalytics(input CallAnalyticsInput) (*CallAnalyticsReport, error)
	GetAdminOverview(input AdminOverviewInput) (*AdminOverview, error)
	GetPlanContractions(input PlanContractionsInput) (*PlanContractionsReport, error)
}

type GetProfitReportUseCase interface {
	Execute(input ProfitReportInput) (*ProfitReport, error)
}

type GetCallAnalyticsUseCase interface {
	Execute(input CallAnalyticsInput) (*CallAnalyticsReport, error)
}
