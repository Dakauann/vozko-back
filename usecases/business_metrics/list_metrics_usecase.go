package business_metrics_usecase

import (
	"errors"
	"time"

	"vozko/domain/business_metrics"
	"vozko/domain/shared"
)

type listMetricsUseCase struct {
	repo business_metrics.Repository
}

func NewListMetricsUseCase(repo business_metrics.Repository) business_metrics.ListMetricsUseCase {
	return &listMetricsUseCase{repo: repo}
}

func (uc *listMetricsUseCase) Execute(input business_metrics.ListMetricsInput) (*business_metrics.ListMetricsOutput, error) {

	page := input.Page
	if page < 1 {
		page = 1
	}

	pageSize := input.PageSize
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 500 {
		pageSize = 500
	}

	var startDate, endDate *time.Time
	if input.StartDate != nil && *input.StartDate != "" {
		parsed, err := time.Parse(time.RFC3339, *input.StartDate)
		if err != nil {
			return nil, errors.New("invalid start_date format, expected ISO 8601 (RFC3339)")
		}
		startDate = &parsed
	}

	if input.EndDate != nil && *input.EndDate != "" {
		parsed, err := time.Parse(time.RFC3339, *input.EndDate)
		if err != nil {
			return nil, errors.New("invalid end_date format, expected ISO 8601 (RFC3339)")
		}
		endDate = &parsed
	}

	var sorts []shared.Sort
	if input.SortBy != "" {
		direction := shared.SortAsc
		if input.SortOrder == "desc" || input.SortOrder == "DESC" {
			direction = shared.SortDesc
		}
		sorts = []shared.Sort{
			{Field: input.SortBy, Direction: direction},
		}
	}

	filters := business_metrics.ListFilters{
		EventTypes: input.EventTypes,
		EntityType: input.EntityType,
		UserID:     input.UserID,
		StartDate:  startDate,
		EndDate:    endDate,
		Pagination: shared.Pagination{
			Page:     page,
			PageSize: pageSize,
		},
		Sorts: sorts,
	}

	result, err := uc.repo.List(filters)
	if err != nil {
		return nil, err
	}

	return &business_metrics.ListMetricsOutput{
		Metrics:     result.Items,
		TotalCount:  result.TotalItems,
		TotalPages:  result.TotalPages,
		CurrentPage: result.Page,
		PageSize:    result.PageSize,
		HasMore:     result.Page < result.TotalPages,
	}, nil
}
