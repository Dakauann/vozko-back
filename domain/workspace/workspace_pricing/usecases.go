package workspace_pricing

type GetDefaultPricingItemsUseCase interface {
	Execute() ([]PricingItem, error)
}

type GetResolvedPricingUseCase interface {
	Execute(workspaceID string) ([]ResolvedPricingItem, error)
}

type UpdatePricingItemUseCase interface {
	Execute(input UpdatePricingItemInput, changedBy string) (*PricingItem, error)
}

type GetPricingAuditLogUseCase interface {
	Execute(workspaceID *string, limit, offset int) ([]PricingAuditEntry, error)
}

type GetExchangeRateUseCase interface {
	Execute() (*PricingItem, error)
}

type UpdateExchangeRateUseCase interface {
	Execute(priceMicros int64, changedBy string) (*PricingItem, error)
}

type UpdatePricingItemInput struct {
	Category    ServiceCategory `json:"category"`
	Service     string          `json:"service"`
	Metric      string          `json:"metric"`
	PriceMicros int64           `json:"priceMicros"`
	Currency    string          `json:"currency"`
}
