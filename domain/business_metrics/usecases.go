package business_metrics

type RecordMetricUseCase interface {
	Execute(input RecordMetricInput) error
}

type ConsumeMetricUseCase interface {
	Start() error
}

type RecordMetricInput struct {
	EventType  MetricEventType
	EntityID   string
	EntityType EntityType
	UserID     *string
	Metadata   map[string]string
}

type ListMetricsUseCase interface {
	Execute(input ListMetricsInput) (*ListMetricsOutput, error)
}

type ListMetricsInput struct {
	EventTypes []MetricEventType
	EntityType *EntityType
	UserID     *string
	StartDate  *string
	EndDate    *string
	Page       int
	PageSize   int
	SortBy     string
	SortOrder  string
}

type ListMetricsOutput struct {
	Metrics     []*Metric `json:"metrics"`
	TotalCount  int64     `json:"total_count"`
	TotalPages  int       `json:"total_pages"`
	CurrentPage int       `json:"current_page"`
	PageSize    int       `json:"page_size"`
	HasMore     bool      `json:"has_more"`
}

type GetMetricsStatsUseCase interface {
	Execute(input GetMetricsStatsInput) (*GetMetricsStatsOutput, error)
}

type GetMetricsStatsInput struct {
	EventTypes []MetricEventType `json:"event_types"`
	StartDate  *string           `json:"start_date"`
	EndDate    *string           `json:"end_date"`
}

type GetMetricsStatsOutput struct {
	EventCounts map[string]int64 `json:"event_counts"`
	TotalEvents int64            `json:"total_events"`
	StartDate   string           `json:"start_date"`
	EndDate     string           `json:"end_date"`
}

type GetMetricsTimeSeriesUseCase interface {
	Execute(input GetMetricsTimeSeriesInput) (*GetMetricsTimeSeriesOutput, error)
}

type TimeSeriesInterval string

const (
	IntervalHour  TimeSeriesInterval = "hour"
	IntervalDay   TimeSeriesInterval = "day"
	IntervalWeek  TimeSeriesInterval = "week"
	IntervalMonth TimeSeriesInterval = "month"
)

type GetMetricsTimeSeriesInput struct {
	EventTypes []MetricEventType  `json:"event_types"`
	Interval   TimeSeriesInterval `json:"interval"`
	StartDate  *string            `json:"start_date"`
	EndDate    *string            `json:"end_date"`
}

type TimeSeriesDataPoint struct {
	Timestamp string `json:"timestamp"`
	Count     int64  `json:"count"`
}

type GetMetricsTimeSeriesOutput struct {
	Data      map[string][]TimeSeriesDataPoint `json:"data"`
	Interval  string                           `json:"interval"`
	StartDate string                           `json:"start_date"`
	EndDate   string                           `json:"end_date"`
}
