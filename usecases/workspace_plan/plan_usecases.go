package workspace_plan_usecase

import (
	"github.com/google/uuid"

	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

func toCatalogEntries(exports []workspace_pricing.CatalogEntryExport) []workspace_plan.CatalogEntry {
	out := make([]workspace_plan.CatalogEntry, len(exports))
	for i, e := range exports {
		out[i] = workspace_plan.CatalogEntry{
			Category:    e.Category,
			Service:     e.Service,
			Metric:      e.Metric,
			CostMicros:  e.CostMicros,
			PriceMicros: e.PriceMicros,
			MarkupPct:   e.MarkupPct,
			Currency:    e.Currency,
		}
	}
	return out
}

func defaultCatalog() []workspace_plan.CatalogEntry {
	return toCatalogEntries(workspace_pricing.ExportDefaultCatalog())
}

type createPlanDefinitionUseCase struct {
	repo workspace_plan.PlanRepository
}

type updatePlanDefinitionUseCase struct {
	repo workspace_plan.PlanRepository
}

type archivePlanDefinitionUseCase struct {
	repo workspace_plan.PlanRepository
	now  clockFn
}

type listPlanDefinitionsUseCase struct {
	repo workspace_plan.PlanRepository
}

type getPlanDefinitionUseCase struct {
	repo workspace_plan.PlanRepository
}

func NewCreatePlanDefinitionUseCase(repo workspace_plan.PlanRepository) workspace_plan.CreatePlanDefinitionUseCase {
	return &createPlanDefinitionUseCase{repo: repo}
}

func NewUpdatePlanDefinitionUseCase(repo workspace_plan.PlanRepository) workspace_plan.UpdatePlanDefinitionUseCase {
	return &updatePlanDefinitionUseCase{repo: repo}
}

func NewArchivePlanDefinitionUseCase(repo workspace_plan.PlanRepository) workspace_plan.ArchivePlanDefinitionUseCase {
	return &archivePlanDefinitionUseCase{repo: repo, now: utcNow}
}

func NewListPlanDefinitionsUseCase(repo workspace_plan.PlanRepository) workspace_plan.ListPlanDefinitionsUseCase {
	return &listPlanDefinitionsUseCase{repo: repo}
}

func NewGetPlanDefinitionUseCase(repo workspace_plan.PlanRepository) workspace_plan.GetPlanDefinitionUseCase {
	return &getPlanDefinitionUseCase{repo: repo}
}

func (uc *createPlanDefinitionUseCase) Execute(input workspace_plan.PlanMutationInput) (*workspace_plan.PlanDefinition, error) {
	isVisible := true
	if input.IsGloballyVisible != nil {
		isVisible = *input.IsGloballyVisible
	}
	plan := &workspace_plan.PlanDefinition{
		ID:                             uuid.NewString(),
		Name:                           input.Name,
		Description:                    input.Description,
		BasePriceBRLCents:              input.BasePriceBRLCents,
		MaxCallChannels:                input.MaxCallChannels,
		IncludedWhatsAppBusinessPhones: input.IncludedWhatsAppBusinessPhones,
		MaxBranches:                    input.MaxBranches,
		MaxHoldMusicTracks:             input.MaxHoldMusicTracks,
		IsGloballyVisible:              isVisible,
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if err := uc.repo.Create(plan); err != nil {
		return nil, err
	}

	catalog := defaultCatalog()
	items := workspace_plan.SeedPricingItemsFromCatalog(plan.ID, catalog)
	items = applyPricingOverrides(items, input.PricingItems)
	assignItemIDs(items)

	if err := uc.repo.ReplacePricingItems(plan.ID, items); err != nil {
		return nil, err
	}
	plan.PricingItems = items
	return plan, nil
}

func (uc *updatePlanDefinitionUseCase) Execute(planID string, input workspace_plan.PlanMutationInput) (*workspace_plan.PlanDefinition, error) {
	plan, err := uc.repo.GetByID(planID)
	if err != nil {
		return nil, err
	}
	if plan.IsArchived() {
		return nil, workspace_plan.ErrPlanArchived
	}
	plan.Name = input.Name
	plan.Description = input.Description
	plan.BasePriceBRLCents = input.BasePriceBRLCents
	plan.MaxCallChannels = input.MaxCallChannels
	plan.IncludedWhatsAppBusinessPhones = input.IncludedWhatsAppBusinessPhones
	plan.MaxBranches = input.MaxBranches
	plan.MaxHoldMusicTracks = input.MaxHoldMusicTracks
	if input.IsGloballyVisible != nil {
		plan.IsGloballyVisible = *input.IsGloballyVisible
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if err := uc.repo.Update(plan); err != nil {
		return nil, err
	}

	catalog := defaultCatalog()
	items := workspace_plan.MergePricingItemsWithCatalog(planID, plan.PricingItems, catalog)
	items = applyPricingOverrides(items, input.PricingItems)
	assignItemIDs(items)

	if err := uc.repo.ReplacePricingItems(planID, items); err != nil {
		return nil, err
	}
	plan.PricingItems = items
	return plan, nil
}

func (uc *archivePlanDefinitionUseCase) Execute(planID string) error {
	return uc.repo.Archive(planID, uc.now())
}

func (uc *listPlanDefinitionsUseCase) Execute(includeArchived bool) ([]*workspace_plan.PlanDefinition, error) {
	plans, err := uc.repo.List(includeArchived)
	if err != nil {
		return nil, err
	}
	catalog := defaultCatalog()
	for _, plan := range plans {
		plan.PricingItems = workspace_plan.MergePricingItemsWithCatalog(plan.ID, plan.PricingItems, catalog)
		if !plan.IsGloballyVisible {
			ids, err := uc.repo.GetAllowedWorkspaceIDs(plan.ID)
			if err != nil {
				return nil, err
			}
			plan.AllowedWorkspaceIDs = ids
			ws, err := uc.repo.GetAllowedWorkspaces(plan.ID)
			if err != nil {
				return nil, err
			}
			plan.AllowedWorkspaces = ws
		}
	}
	return plans, nil
}

func (uc *getPlanDefinitionUseCase) Execute(planID string) (*workspace_plan.PlanDefinition, error) {
	plan, err := uc.repo.GetByID(planID)
	if err != nil {
		return nil, err
	}
	catalog := defaultCatalog()
	plan.PricingItems = workspace_plan.MergePricingItemsWithCatalog(plan.ID, plan.PricingItems, catalog)
	if !plan.IsGloballyVisible {
		ids, err := uc.repo.GetAllowedWorkspaceIDs(plan.ID)
		if err != nil {
			return nil, err
		}
		plan.AllowedWorkspaceIDs = ids
		ws, err := uc.repo.GetAllowedWorkspaces(plan.ID)
		if err != nil {
			return nil, err
		}
		plan.AllowedWorkspaces = ws
	}
	return plan, nil
}

func applyPricingOverrides(items []workspace_plan.PlanPricingItem, overrides []workspace_plan.PlanPricingItemInput) []workspace_plan.PlanPricingItem {
	if len(overrides) == 0 {
		return items
	}
	overrideMap := make(map[string]*workspace_plan.PlanPricingItemInput, len(overrides))
	for i := range overrides {
		k := overrides[i].Category + "|" + overrides[i].Service + "|" + overrides[i].Metric
		overrideMap[k] = &overrides[i]
	}
	for i := range items {
		k := items[i].Category + "|" + items[i].Service + "|" + items[i].Metric
		if o, ok := overrideMap[k]; ok {
			items[i].CostMicros = o.CostMicros
			items[i].PriceMicros = o.PriceMicros
			items[i].MarkupPct = o.MarkupPct
			if o.Currency != "" {
				items[i].Currency = o.Currency
			}
		}
	}
	return items
}

func assignItemIDs(items []workspace_plan.PlanPricingItem) {
	for i := range items {
		if items[i].ID == "" {
			items[i].ID = uuid.NewString()
		}
	}
}

type setPlanVisibilityUseCase struct {
	repo workspace_plan.PlanRepository
}

type listVisiblePlansUseCase struct {
	repo      workspace_plan.PlanRepository
	referrals workspace_plan.WorkspaceReferralReader
}

func NewSetPlanVisibilityUseCase(repo workspace_plan.PlanRepository) workspace_plan.SetPlanVisibilityUseCase {
	return &setPlanVisibilityUseCase{repo: repo}
}

func NewListVisiblePlansUseCase(
	repo workspace_plan.PlanRepository,
	referrals workspace_plan.WorkspaceReferralReader,
) workspace_plan.ListVisiblePlansUseCase {
	return &listVisiblePlansUseCase{repo: repo, referrals: referrals}
}

func (uc *setPlanVisibilityUseCase) Execute(planID string, input workspace_plan.SetPlanVisibilityInput) (*workspace_plan.PlanDefinition, error) {
	plan, err := uc.repo.GetByID(planID)
	if err != nil {
		return nil, err
	}

	if plan.IsExclusive() {
		return nil, workspace_plan.ErrPlanExclusiveLocked
	}
	plan.IsGloballyVisible = input.IsGloballyVisible
	if err := uc.repo.Update(plan); err != nil {
		return nil, err
	}
	if !input.IsGloballyVisible {
		if err := uc.repo.SetVisibility(planID, input.AllowedWorkspaceIDs); err != nil {
			return nil, err
		}
		plan.AllowedWorkspaceIDs = input.AllowedWorkspaceIDs
		ws, err := uc.repo.GetAllowedWorkspaces(planID)
		if err != nil {
			return nil, err
		}
		plan.AllowedWorkspaces = ws
	} else {
		if err := uc.repo.SetVisibility(planID, nil); err != nil {
			return nil, err
		}
		plan.AllowedWorkspaceIDs = nil
		plan.AllowedWorkspaces = nil
	}
	catalog := defaultCatalog()
	plan.PricingItems = workspace_plan.MergePricingItemsWithCatalog(plan.ID, plan.PricingItems, catalog)
	return plan, nil
}

func (uc *listVisiblePlansUseCase) Execute(workspaceID string) ([]*workspace_plan.PlanDefinition, error) {
	plans, err := uc.repo.ListVisiblePlans(workspaceID)
	if err != nil {
		return nil, err
	}

	if uc.referrals != nil && workspaceID != "" {
		affiliateID, err := uc.referrals.GetAffiliateIDByWorkspaceID(workspaceID)
		if err != nil {
			return nil, err
		}
		if affiliateID != "" {
			exclusive, err := uc.repo.ListByExclusiveAffiliateID(affiliateID)
			if err != nil {
				return nil, err
			}
			plans = append(plans, exclusive...)
		}
	}
	catalog := defaultCatalog()
	for _, plan := range plans {
		plan.PricingItems = workspace_plan.MergePricingItemsWithCatalog(plan.ID, plan.PricingItems, catalog)
	}
	return plans, nil
}
