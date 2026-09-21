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

// Execute normalizes everything the repository is about to interpolate or bind,
// then delegates. Nothing downstream re-validates, so this is the only place
// where a sort field stops being free text and a page size stops being
// unbounded.
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

	// InferredOnly is deliberately NOT set here. Whether the counts are still an
	// inference depends on how much of the period Meta has answered for, which
	// only the query knows, so the repository derives it from coverage. Setting
	// it here would have meant the page carried an "upper bound" caveat forever,
	// including after Meta had confirmed every message in the period.
	return uc.repo.GetMetaServiceMessageCost(input)
}

// normalizeCostPeriod defaults an unstated range to the current calendar
// month and repairs a reversed one.
//
// The month is computed in the billing timezone rather than UTC, because that
// is the month the operator reading this page is accruing cost in, and it is
// already how the rest of the product decides what a month is. The upper bound
// is the first instant of the NEXT month, which pairs with the repository's
// half-open created_at range so a message at 23:59:59.999 on the last day is
// neither counted twice nor lost.
func normalizeCostPeriod(start, end time.Time) (time.Time, time.Time) {
	if start.IsZero() || end.IsZero() {
		loc := billing.LocationBRT()
		now := time.Now().In(loc)
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		return monthStart, monthStart.AddDate(0, 1, 0)
	}

	// A reversed range silently returns zero for every workspace, which reads as
	// "nobody sent anything". That is the most dangerous wrong answer this page
	// can give, so repair it rather than report it.
	if end.Before(start) {
		return end, start
	}

	return start, end
}
