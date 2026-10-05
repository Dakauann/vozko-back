package analytics_usecase

import (
	"fmt"
	"time"

	analytics_domain "vozko/domain/analytics"
	"vozko/domain/billing"
	"vozko/domain/shared"
	"vozko/domain/workspace/workspace_pricing"
)

type DefaultPricingCatalog interface {
	ListDefaultPricingItems() ([]workspace_pricing.PricingItem, error)
}

type WorkspacePricingResolver interface {
	ResolveForWorkspace(workspaceID string) ([]workspace_pricing.ResolvedPricingItem, error)
}

type getMetaServiceMessageCostUseCase struct {
	repo    analytics_domain.Repository
	catalog DefaultPricingCatalog
	pricing WorkspacePricingResolver
}

func NewGetMetaServiceMessageCostUseCase(repo analytics_domain.Repository, catalog DefaultPricingCatalog, pricing WorkspacePricingResolver) analytics_domain.GetMetaServiceMessageCostUseCase {
	return &getMetaServiceMessageCostUseCase{repo: repo, catalog: catalog, pricing: pricing}
}

func (uc *getMetaServiceMessageCostUseCase) Execute(input analytics_domain.MetaServiceMessageCostInput) (*analytics_domain.MetaServiceMessageCostReport, error) {
	input.StartDate, input.EndDate = normalizeCostPeriod(input.StartDate, input.EndDate)
	input.Provider = input.Provider.Normalized()
	input.SortBy = analytics_domain.NormalizeMetaServiceMessageCostSortField(string(input.SortBy))

	if input.SortOrder != shared.SortAsc {
		input.SortOrder = shared.SortDesc
	}

	pagination := shared.NormalizePagination(shared.Pagination{Page: input.Page, PageSize: input.PageSize})
	input.Page = pagination.Page
	input.PageSize = pagination.PageSize

	rates, err := costRates(uc.catalog)
	if err != nil {
		return nil, err
	}
	report, err := uc.repo.GetMetaServiceMessageCost(input)
	if err != nil {
		return nil, err
	}
	numbers, err := uc.repo.MetaCostNumbers(input)
	if err != nil {
		return nil, err
	}
	unlinked, err := uc.repo.UnlinkedServiceMessages(input)
	if err != nil {
		return nil, err
	}
	costs, err := uc.serviceMessageCosts(report.Totals.ChargedWorkspaceIDs())
	if err != nil {
		return nil, err
	}
	report.AttachDetails(numbers, unlinked)
	report.ApplyRates(rates, costs)
	return report, nil
}

func (uc *getMetaServiceMessageCostUseCase) serviceMessageCosts(workspaceIDs []string) (analytics_domain.ServiceMessageCosts, error) {
	costs := make(analytics_domain.ServiceMessageCosts, len(workspaceIDs))
	for _, id := range workspaceIDs {
		resolved, err := uc.pricing.ResolveForWorkspace(id)
		if err != nil {
			return nil, fmt.Errorf("meta cost service price of workspace %s: %w", id, err)
		}
		costs[id] = workspace_pricing.ServiceMessageCostMicros(resolved)
	}
	return costs, nil
}

func costRates(catalog DefaultPricingCatalog) (analytics_domain.CostRates, error) {
	items, err := catalog.ListDefaultPricingItems()
	if err != nil {
		return analytics_domain.CostRates{}, fmt.Errorf("meta cost rates: %w", err)
	}
	usdToBRL, _ := workspace_pricing.USDToBRLMicros(items)
	return analytics_domain.CostRates{USDToBRLMicros: usdToBRL}, nil
}

func normalizeCostPeriod(start, end time.Time) (time.Time, time.Time) {
	if start.IsZero() || end.IsZero() {
		loc := billing.LocationBRT()
		now := time.Now().In(loc)
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		return monthStart, monthStart.AddDate(0, 1, 0)
	}

	if end.Before(start) {
		return end, start
	}

	return start, end
}
