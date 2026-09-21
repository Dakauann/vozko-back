package analytics_usecase

import (
	"time"

	analytics_domain "vozko/domain/analytics"
	"vozko/domain/billing"
	"vozko/domain/shared"
)

type getMetaServiceMessageCostUseCase struct {
	repo analytics_domain.Repository
}

func NewGetMetaServiceMessageCostUseCase(repo analytics_domain.Repository) analytics_domain.GetMetaServiceMessageCostUseCase {
	return &getMetaServiceMessageCostUseCase{repo: repo}
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

	return uc.repo.GetMetaServiceMessageCost(input)
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
