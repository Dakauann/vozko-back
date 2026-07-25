package business_metrics

import (
	"time"

	"vozko/domain/shared"
)

type Repository interface {
	Record(metric *Metric) error

	List(filters ListFilters) (*shared.PaginatedResult[*Metric], error)

	GetStats(filters StatsFilters) (*Stats, error)

	GetTimeSeries(filters TimeSeriesFilters) ([]TimeSeriesPoint, error)
}

type ListFilters struct {
	EventTypes []MetricEventType
	EntityType *EntityType
	UserID     *string
	StartDate  *time.Time
	EndDate    *time.Time
	Pagination shared.Pagination
	Sorts      []shared.Sort
}

type StatsFilters struct {
	EventTypes []MetricEventType
	StartDate  *time.Time
	EndDate    *time.Time
}

type Stats struct {
	EventCounts map[MetricEventType]int64
	TotalEvents int64
	StartDate   time.Time
	EndDate     time.Time
}

type TimeSeriesFilters struct {
	EventTypes []MetricEventType
	Interval   string
	StartDate  *time.Time
	EndDate    *time.Time
}

type TimeSeriesPoint struct {
	EventType MetricEventType
	Timestamp time.Time
	Count     int64
}
