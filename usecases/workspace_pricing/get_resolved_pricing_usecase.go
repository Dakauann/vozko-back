package workspace_pricing_usecase

import (
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type getResolvedPricingUseCase struct {
	pricer workspace_pricing.Pricer
}

func NewGetResolvedPricingUseCase(pricer workspace_pricing.Pricer) workspace_pricing.GetResolvedPricingUseCase {
	return &getResolvedPricingUseCase{pricer: pricer}
}

func (uc *getResolvedPricingUseCase) Execute(workspaceID string) ([]workspace_pricing.ResolvedPricingItem, error) {
	return uc.pricer.ResolveForWorkspace(workspaceID)
}
