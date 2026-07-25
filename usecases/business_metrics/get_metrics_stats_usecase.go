package business_metrics_usecase

import (
	"errors"
	"time"

	"vozko/domain/business_metrics"
)

type getMetricsStatsUseCase struct {
	repo business_metrics.Repository
}

func NewGetMetricsStatsUseCase(repo business_metrics.Repository) business_metrics.GetMetricsStatsUseCase {
	return &getMetricsStatsUseCase{repo: repo}
}

func (uc *getMetricsStatsUseCase) Execute(input business_metrics.GetMetricsStatsInput) (*business_metrics.GetMetricsStatsOutput, error) {
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

	filters := business_metrics.StatsFilters{
		EventTypes: input.EventTypes,
		StartDate:  startDate,
		EndDate:    endDate,
	}

	stats, err := uc.repo.GetStats(filters)
	if err != nil {
		return nil, err
	}

	eventCountsStr := make(map[string]int64)
	for eventType, count := range stats.EventCounts {
		eventCountsStr[string(eventType)] = count
	}

	return &business_metrics.GetMetricsStatsOutput{
		EventCounts: eventCountsStr,
		TotalEvents: stats.TotalEvents,
		StartDate:   stats.StartDate.Format(time.RFC3339),
		EndDate:     stats.EndDate.Format(time.RFC3339),
	}, nil
}
