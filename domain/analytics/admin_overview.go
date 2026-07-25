package analytics

import (
	"time"

	"vozko/domain/shared"
)

type WorkspaceFinancialSnapshot struct {
	WorkspaceID          string     `json:"workspaceId"`
	WorkspaceName        string     `json:"workspaceName"`
	CurrentBalanceMicros int64      `json:"currentBalanceMicros"`
	PeriodCreditsMicros  int64      `json:"periodCreditsMicros"`
	RevenueMicros        int64      `json:"revenueMicros"`
	CostMicros           int64      `json:"costMicros"`
	ProfitMicros         int64      `json:"profitMicros"`
	RevenueBRLMicros     int64      `json:"revenueBRLMicros"`
	CostBRLMicros        int64      `json:"costBRLMicros"`
	ProfitBRLMicros      int64      `json:"profitBRLMicros"`
	RefundsMicros        int64      `json:"refundsMicros"`
	RefundsBRLMicros     int64      `json:"refundsBRLMicros"`
	MarginPct            float64    `json:"marginPct"`
	TxCount              int64      `json:"txCount"`
	LastTransactionAt    *time.Time `json:"lastTransactionAt,omitempty"`
}

type FocusedWorkspaceBalance struct {
	WorkspaceID           string     `json:"workspaceId"`
	WorkspaceName         string     `json:"workspaceName"`
	CurrentBalanceMicros  int64      `json:"currentBalanceMicros"`
	LifetimeCreditsMicros int64      `json:"lifetimeCreditsMicros"`
	LifetimeDebitsMicros  int64      `json:"lifetimeDebitsMicros"`
	PeriodCreditsMicros   int64      `json:"periodCreditsMicros"`
	PeriodDebitsMicros    int64      `json:"periodDebitsMicros"`
	PeriodTxCount         int64      `json:"periodTxCount"`
	LastTransactionAt     *time.Time `json:"lastTransactionAt,omitempty"`
}

type RecentSpending struct {
	TransactionID      string    `json:"transactionId"`
	WorkspaceID        string    `json:"workspaceId"`
	WorkspaceName      string    `json:"workspaceName"`
	ServiceType        string    `json:"serviceType"`
	Description        string    `json:"description"`
	AmountMicros       int64     `json:"amountMicros"`
	CostMicros         int64     `json:"costMicros"`
	ProfitMicros       int64     `json:"profitMicros"`
	ExchangeRateMicros int64     `json:"exchangeRateMicros"`
	BalanceAfterMicros int64     `json:"balanceAfterMicros"`
	CreatedAt          time.Time `json:"createdAt"`
}

type UsageMetric struct {
	Key              string `json:"key"`
	Count            int64  `json:"count"`
	RevenueMicros    int64  `json:"revenueMicros,omitempty"`
	RevenueBRLMicros int64  `json:"revenueBRLMicros,omitempty"`
}

type ProductUsageSummary struct {
	ByService                  []UsageMetric `json:"byService"`
	WhatsAppTemplateCategories []UsageMetric `json:"whatsappTemplateCategories"`
}

type AdminOverview struct {
	Period                      Period                                               `json:"period"`
	FocusedWorkspace            *FocusedWorkspaceBalance                             `json:"focusedWorkspace,omitempty"`
	WorkspaceSnapshots          *shared.PaginatedResult[*WorkspaceFinancialSnapshot] `json:"workspaceSnapshots"`
	RecentSpendings             []RecentSpending                                     `json:"recentSpendings"`
	ProductUsage                ProductUsageSummary                                  `json:"productUsage"`
	TotalPeriodCreditsMicros    int64                                                `json:"totalPeriodCreditsMicros"`
	TotalPeriodCreditsBRLMicros int64                                                `json:"totalPeriodCreditsBRLMicros"`
	TotalPeriodRefundsMicros    int64                                                `json:"totalPeriodRefundsMicros"`
	TotalPeriodRefundsBRLMicros int64                                                `json:"totalPeriodRefundsBRLMicros"`
}

type AdminOverviewInput struct {
	WorkspaceID        *string
	StartDate          time.Time
	EndDate            time.Time
	WorkspaceSearch    string
	WorkspacePage      int
	WorkspacePageSize  int
	WorkspaceSortBy    string
	WorkspaceSortOrder string
	RecentLimit        int

	ServiceTypes       []string
	PlanDefinitionID   *string
	BillingCycle       *string
	SubscriptionStatus *string
}

type GetAdminOverviewUseCase interface {
	Execute(input AdminOverviewInput) (*AdminOverview, error)
}
