package businessmetrics

import (
	"time"

	businessmetricsdomain "vozko/domain/business_metrics"
)

type MetricResponse struct {
	ID         string            `json:"id"`
	EventType  string            `json:"event_type"`
	EntityID   string            `json:"entity_id"`
	EntityType string            `json:"entity_type"`
	UserID     *string           `json:"user_id"`
	Metadata   map[string]string `json:"metadata"`
	OccurredAt time.Time         `json:"occurred_at"`
	CreatedAt  time.Time         `json:"created_at"`
}

type ListMetricsResponse struct {
	Metrics     []MetricResponse `json:"metrics"`
	TotalCount  int64            `json:"total_count"`
	TotalPages  int              `json:"total_pages"`
	CurrentPage int              `json:"current_page"`
	PageSize    int              `json:"page_size"`
	HasMore     bool             `json:"has_more"`
}

type MetricsStatsResponse struct {
	EventCounts map[string]int64 `json:"event_counts"`
	TotalEvents int64            `json:"total_events"`
	StartDate   string           `json:"start_date"`
	EndDate     string           `json:"end_date"`
}

type TimeSeriesDataPointResponse struct {
	Timestamp string `json:"timestamp"`
	Count     int64  `json:"count"`
}

type MetricsTimeSeriesResponse struct {
	Data      map[string][]TimeSeriesDataPointResponse `json:"data"`
	Interval  string                                   `json:"interval"`
	StartDate string                                   `json:"start_date"`
	EndDate   string                                   `json:"end_date"`
}

func toMetricResponse(m *businessmetricsdomain.Metric) MetricResponse {
	return MetricResponse{
		ID:         m.ID,
		EventType:  string(m.EventType),
		EntityID:   m.EntityID,
		EntityType: string(m.EntityType),
		UserID:     m.UserID,
		Metadata:   m.Metadata,
		OccurredAt: m.OccurredAt,
		CreatedAt:  m.CreatedAt,
	}
}

func toListMetricsResponse(out *businessmetricsdomain.ListMetricsOutput) ListMetricsResponse {
	var metrics []MetricResponse
	if out.Metrics != nil {
		metrics = make([]MetricResponse, len(out.Metrics))
		for i, m := range out.Metrics {
			metrics[i] = toMetricResponse(m)
		}
	}
	return ListMetricsResponse{
		Metrics:     metrics,
		TotalCount:  out.TotalCount,
		TotalPages:  out.TotalPages,
		CurrentPage: out.CurrentPage,
		PageSize:    out.PageSize,
		HasMore:     out.HasMore,
	}
}

func toMetricsStatsResponse(out *businessmetricsdomain.GetMetricsStatsOutput) MetricsStatsResponse {
	return MetricsStatsResponse{
		EventCounts: out.EventCounts,
		TotalEvents: out.TotalEvents,
		StartDate:   out.StartDate,
		EndDate:     out.EndDate,
	}
}

func toMetricsTimeSeriesResponse(out *businessmetricsdomain.GetMetricsTimeSeriesOutput) MetricsTimeSeriesResponse {
	var data map[string][]TimeSeriesDataPointResponse
	if out.Data != nil {
		data = make(map[string][]TimeSeriesDataPointResponse, len(out.Data))
		for key, points := range out.Data {
			var converted []TimeSeriesDataPointResponse
			if points != nil {
				converted = make([]TimeSeriesDataPointResponse, len(points))
				for i, p := range points {
					converted[i] = TimeSeriesDataPointResponse{
						Timestamp: p.Timestamp,
						Count:     p.Count,
					}
				}
			}
			data[key] = converted
		}
	}
	return MetricsTimeSeriesResponse{
		Data:      data,
		Interval:  out.Interval,
		StartDate: out.StartDate,
		EndDate:   out.EndDate,
	}
}
