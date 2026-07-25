package analytics

import "time"

type PlanContraction struct {
	SubscriptionID string    `json:"subscriptionId"`
	WorkspaceID    string    `json:"workspaceId"`
	WorkspaceName  string    `json:"workspaceName"`
	PlanName       string    `json:"planName"`
	BillingCycle   string    `json:"billingCycle"`
	Status         string    `json:"status"`
	ContractedAt   time.Time `json:"contractedAt"`
}

type PlanContractionCount struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

type PlanContractionBucket struct {
	PeriodStart time.Time `json:"periodStart"`
	PeriodEnd   time.Time `json:"periodEnd"`
	Count       int64     `json:"count"`
}

type PlanContractionsReport struct {
	Period         Period                  `json:"period"`
	TotalCount     int64                   `json:"totalCount"`
	ByPlan         []PlanContractionCount  `json:"byPlan"`
	ByBillingCycle []PlanContractionCount  `json:"byBillingCycle"`
	TimeSeries     []PlanContractionBucket `json:"timeSeries"`
	Recent         []PlanContraction       `json:"recent"`
}

type PlanContractionsInput struct {
	StartDate   time.Time
	EndDate     time.Time
	Granularity Granularity
	RecentLimit int
}

type GetPlanContractionsUseCase interface {
	Execute(input PlanContractionsInput) (*PlanContractionsReport, error)
}
