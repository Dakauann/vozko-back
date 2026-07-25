package workspace_plan_usecase

import (
	"context"
	"errors"
	"strings"

	"vozko/domain/affiliate"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type setPlanExclusiveAffiliateUseCase struct {
	plans      workspace_plan.PlanRepository
	affiliates affiliate.Repository
}

func NewSetPlanExclusiveAffiliateUseCase(
	plans workspace_plan.PlanRepository,
	affiliates affiliate.Repository,
) workspace_plan.SetPlanExclusiveAffiliateUseCase {
	return &setPlanExclusiveAffiliateUseCase{plans: plans, affiliates: affiliates}
}

func (uc *setPlanExclusiveAffiliateUseCase) Execute(
	planID string,
	input workspace_plan.SetPlanExclusiveAffiliateInput,
) (*workspace_plan.PlanDefinition, error) {
	if strings.TrimSpace(planID) == "" {
		return nil, workspace_plan.ErrPlanNotFound
	}
	plan, err := uc.plans.GetByID(planID)
	if err != nil {
		return nil, err
	}
	if plan.IsArchived() {
		return nil, workspace_plan.ErrPlanArchived
	}

	if input.AffiliateID == nil || strings.TrimSpace(*input.AffiliateID) == "" {
		if err := uc.plans.SetExclusiveAffiliate(planID, nil); err != nil {
			return nil, err
		}
		plan.ExclusiveAffiliateID = nil
		plan.IsGloballyVisible = true
		if err := uc.plans.Update(plan); err != nil {
			return nil, err
		}
		if err := uc.plans.SetVisibility(planID, nil); err != nil {
			return nil, err
		}
		plan.AllowedWorkspaceIDs = nil
		plan.AllowedWorkspaces = nil
		return plan, nil
	}

	affiliateID := strings.TrimSpace(*input.AffiliateID)
	aff, err := uc.affiliates.GetByID(context.Background(), affiliateID)
	if err != nil {
		if errors.Is(err, affiliate.ErrAffiliateNotFound) {
			return nil, workspace_plan.ErrExclusiveAffiliateRequired
		}
		return nil, err
	}
	if aff == nil || !aff.Active {
		return nil, workspace_plan.ErrExclusiveAffiliateRequired
	}

	plan.IsGloballyVisible = false
	if err := uc.plans.Update(plan); err != nil {
		return nil, err
	}
	if err := uc.plans.SetVisibility(planID, nil); err != nil {
		return nil, err
	}
	if err := uc.plans.SetExclusiveAffiliate(planID, &affiliateID); err != nil {
		return nil, err
	}

	plan.ExclusiveAffiliateID = &affiliateID
	plan.AllowedWorkspaceIDs = nil
	plan.AllowedWorkspaces = nil
	return plan, nil
}

type listExclusivePlansByAffiliateCodeUseCase struct {
	plans      workspace_plan.PlanRepository
	affiliates affiliate.Repository
}

func NewListExclusivePlansByAffiliateCodeUseCase(
	plans workspace_plan.PlanRepository,
	affiliates affiliate.Repository,
) workspace_plan.ListExclusivePlansByAffiliateCodeUseCase {
	return &listExclusivePlansByAffiliateCodeUseCase{plans: plans, affiliates: affiliates}
}

func (uc *listExclusivePlansByAffiliateCodeUseCase) Execute(code string) ([]*workspace_plan.PlanDefinition, error) {
	normalized := affiliate.NormalizeCode(code)
	if normalized == "" {
		return []*workspace_plan.PlanDefinition{}, nil
	}
	aff, err := uc.affiliates.GetByCode(context.Background(), normalized)
	if err != nil {
		if errors.Is(err, affiliate.ErrAffiliateNotFound) {
			return []*workspace_plan.PlanDefinition{}, nil
		}
		return nil, err
	}
	if aff == nil || !aff.Active {
		return []*workspace_plan.PlanDefinition{}, nil
	}
	plans, err := uc.plans.ListByExclusiveAffiliateID(aff.ID)
	if err != nil {
		return nil, err
	}
	catalog := defaultCatalog()
	for _, plan := range plans {
		plan.PricingItems = workspace_plan.MergePricingItemsWithCatalog(plan.ID, plan.PricingItems, catalog)
	}
	return plans, nil
}

type listMyExclusivePlansUseCase struct {
	plans      workspace_plan.PlanRepository
	affiliates affiliate.Repository
}

func NewListMyExclusivePlansUseCase(
	plans workspace_plan.PlanRepository,
	affiliates affiliate.Repository,
) workspace_plan.ListMyExclusivePlansUseCase {
	return &listMyExclusivePlansUseCase{plans: plans, affiliates: affiliates}
}

func (uc *listMyExclusivePlansUseCase) Execute(userID string) ([]*workspace_plan.PlanDefinition, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, workspace_plan.ErrAffiliateRequired
	}
	aff, err := uc.affiliates.GetByUserID(context.Background(), userID)
	if err != nil {
		if errors.Is(err, affiliate.ErrAffiliateNotFound) {
			return nil, workspace_plan.ErrAffiliateRequired
		}
		return nil, err
	}
	if aff == nil || !aff.Active {
		return nil, workspace_plan.ErrAffiliateRequired
	}
	plans, err := uc.plans.ListByExclusiveAffiliateID(aff.ID)
	if err != nil {
		return nil, err
	}
	catalog := defaultCatalog()
	for _, plan := range plans {
		plan.PricingItems = workspace_plan.MergePricingItemsWithCatalog(plan.ID, plan.PricingItems, catalog)
	}
	return plans, nil
}
