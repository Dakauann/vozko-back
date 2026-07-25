package business_metrics_usecase

import (
	"errors"
	"time"

	"vozko/domain/business_metrics"
)

type getMetricsTimeSeriesUseCase struct {
	repo business_metrics.Repository
}

func NewGetMetricsTimeSeriesUseCase(repo business_metrics.Repository) business_metrics.GetMetricsTimeSeriesUseCase {
	return &getMetricsTimeSeriesUseCase{repo: repo}
}

func (uc *getMetricsTimeSeriesUseCase) Execute(input business_metrics.GetMetricsTimeSeriesInput) (*business_metrics.GetMetricsTimeSeriesOutput, error) {

	validIntervals := map[business_metrics.TimeSeriesInterval]bool{
		business_metrics.IntervalHour:  true,
		business_metrics.IntervalDay:   true,
		business_metrics.IntervalWeek:  true,
		business_metrics.IntervalMonth: true,
	}
	if !validIntervals[input.Interval] {
		return nil, errors.New("invalid interval: must be hour, day, week, or month")
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

	if startDate == nil {
		defaultStart := time.Now().Add(-24 * time.Hour)
		startDate = &defaultStart
	}
	if endDate == nil {
		now := time.Now()
		endDate = &now
	}

	filters := business_metrics.TimeSeriesFilters{
		EventTypes: input.EventTypes,
		Interval:   string(input.Interval),
		StartDate:  startDate,
		EndDate:    endDate,
	}

	points, err := uc.repo.GetTimeSeries(filters)
	if err != nil {
		return nil, err
	}

	dataByEventType := make(map[string][]business_metrics.TimeSeriesDataPoint)
	for _, point := range points {
		eventTypeStr := string(point.EventType)
		dataPoint := business_metrics.TimeSeriesDataPoint{
			Timestamp: point.Timestamp.Format(time.RFC3339),
			Count:     point.Count,
		}
		dataByEventType[eventTypeStr] = append(dataByEventType[eventTypeStr], dataPoint)
	}

	return &business_metrics.GetMetricsTimeSeriesOutput{
		Data:      dataByEventType,
		Interval:  string(input.Interval),
		StartDate: startDate.Format(time.RFC3339),
		EndDate:   endDate.Format(time.RFC3339),
	}, nil
}
