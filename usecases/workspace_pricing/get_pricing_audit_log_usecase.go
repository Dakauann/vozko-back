package workspace_pricing_usecase

import (
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type getPricingAuditLogUseCase struct {
	repo workspace_pricing.Repository
}

func NewGetPricingAuditLogUseCase(repo workspace_pricing.Repository) workspace_pricing.GetPricingAuditLogUseCase {
	return &getPricingAuditLogUseCase{repo: repo}
}

func (uc *getPricingAuditLogUseCase) Execute(workspaceID *string, limit, offset int) ([]workspace_pricing.PricingAuditEntry, error) {
	return uc.repo.ListAuditEntries(workspaceID, limit, offset)
}
