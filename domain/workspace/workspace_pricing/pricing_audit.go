package workspace_pricing

import "time"

type PricingAuditEntry struct {
	ID             string          `json:"id"`
	WorkspaceID    *string         `json:"workspaceId"`
	Category       ServiceCategory `json:"category"`
	Service        string          `json:"service"`
	Metric         string          `json:"metric"`
	OldPriceMicros int64           `json:"oldPriceMicros"`
	NewPriceMicros int64           `json:"newPriceMicros"`
	OldCostMicros  int64           `json:"oldCostMicros"`
	NewCostMicros  int64           `json:"newCostMicros"`
	Currency       string          `json:"currency"`
	ChangedBy      string          `json:"changedBy"`
	ChangedAt      time.Time       `json:"changedAt"`
}
