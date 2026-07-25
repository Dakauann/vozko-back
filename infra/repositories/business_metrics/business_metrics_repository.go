package business_metrics_repository

import (
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/business_metrics"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewBusinessMetricsRepository(db *gorm.DB) business_metrics.Repository {
	return &repository{db: db}
}

func (r *repository) Record(metric *business_metrics.Metric) error {
	record := mapToSchema(metric)
	if err := r.db.Create(&record).Error; err != nil {
		return err
	}
	metric.ID = record.ID
	metric.CreatedAt = record.CreatedAt
	return nil
}

func (r *repository) List(filters business_metrics.ListFilters) (*shared.PaginatedResult[*business_metrics.Metric], error) {
	query := r.db.Model(&schema.BusinessMetric{})

	query = applyFilters(query, filters)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	query = applySorts(query, filters.Sorts)

	offset := (filters.Pagination.Page - 1) * filters.Pagination.PageSize
	query = query.Offset(offset).Limit(filters.Pagination.PageSize)

	var records []schema.BusinessMetric
	if err := query.Find(&records).Error; err != nil {
		return nil, err
	}

	metrics := make([]*business_metrics.Metric, len(records))
	for i, record := range records {
		metrics[i] = mapToDomain(&record)
	}

	return shared.NewPaginatedResult(metrics, filters.Pagination, total), nil
}

func (r *repository) GetStats(filters business_metrics.StatsFilters) (*business_metrics.Stats, error) {
	query := r.db.Model(&schema.BusinessMetric{})

	if filters.StartDate != nil {
		query = query.Where("occurred_at >= ?", filters.StartDate)
	}
	if filters.EndDate != nil {
		query = query.Where("occurred_at <= ?", filters.EndDate)
	}

	if len(filters.EventTypes) > 0 {
		eventTypes := make([]string, len(filters.EventTypes))
		for i, et := range filters.EventTypes {
			eventTypes[i] = string(et)
		}
		query = query.Where("event_type IN ?", eventTypes)
	}

	var totalEvents int64
	if err := query.Count(&totalEvents).Error; err != nil {
		return nil, err
	}

	type eventCount struct {
		EventType string
		Count     int64
	}
	var eventCounts []eventCount
	if err := query.Select("event_type, COUNT(*) as count").
		Group("event_type").
		Scan(&eventCounts).Error; err != nil {
		return nil, err
	}

	eventCountsMap := make(map[business_metrics.MetricEventType]int64)
	for _, ec := range eventCounts {
		eventCountsMap[business_metrics.MetricEventType(ec.EventType)] = ec.Count
	}

	startDate := time.Now().AddDate(0, 0, -30)
	if filters.StartDate != nil {
		startDate = *filters.StartDate
	}

	endDate := time.Now()
	if filters.EndDate != nil {
		endDate = *filters.EndDate
	}

	return &business_metrics.Stats{
		EventCounts: eventCountsMap,
		TotalEvents: totalEvents,
		StartDate:   startDate,
		EndDate:     endDate,
	}, nil
}

func (r *repository) GetTimeSeries(filters business_metrics.TimeSeriesFilters) ([]business_metrics.TimeSeriesPoint, error) {
	query := r.db.Model(&schema.BusinessMetric{})

	if filters.StartDate != nil {
		query = query.Where("occurred_at >= ?", filters.StartDate)
	}
	if filters.EndDate != nil {
		query = query.Where("occurred_at <= ?", filters.EndDate)
	}

	if len(filters.EventTypes) > 0 {
		eventTypes := make([]string, len(filters.EventTypes))
		for i, et := range filters.EventTypes {
			eventTypes[i] = string(et)
		}
		query = query.Where("event_type IN ?", eventTypes)
	}

	var truncateFormat string
	switch filters.Interval {
	case "hour":
		truncateFormat = "DATE_TRUNC('hour', occurred_at)"
	case "day":
		truncateFormat = "DATE_TRUNC('day', occurred_at)"
	case "week":
		truncateFormat = "DATE_TRUNC('week', occurred_at)"
	case "month":
		truncateFormat = "DATE_TRUNC('month', occurred_at)"
	default:
		truncateFormat = "DATE_TRUNC('hour', occurred_at)"
	}

	type timeCount struct {
		EventType string
		Timestamp time.Time
		Count     int64
	}
	var timeCounts []timeCount

	if err := query.Select("event_type, " + truncateFormat + " as timestamp, COUNT(*) as count").
		Group("event_type, timestamp").
		Order("event_type ASC, timestamp ASC").
		Scan(&timeCounts).Error; err != nil {
		return nil, err
	}

	points := make([]business_metrics.TimeSeriesPoint, len(timeCounts))
	for i, tc := range timeCounts {
		points[i] = business_metrics.TimeSeriesPoint{
			EventType: business_metrics.MetricEventType(tc.EventType),
			Timestamp: tc.Timestamp,
			Count:     tc.Count,
		}
	}

	return points, nil
}

func applyFilters(query *gorm.DB, filters business_metrics.ListFilters) *gorm.DB {
	if len(filters.EventTypes) > 0 {
		eventTypes := make([]string, len(filters.EventTypes))
		for i, et := range filters.EventTypes {
			eventTypes[i] = string(et)
		}
		query = query.Where("event_type IN ?", eventTypes)
	}

	if filters.EntityType != nil {
		query = query.Where("entity_type = ?", string(*filters.EntityType))
	}

	if filters.UserID != nil {
		query = query.Where("user_id = ?", *filters.UserID)
	}

	if filters.StartDate != nil {
		query = query.Where("occurred_at >= ?", *filters.StartDate)
	}

	if filters.EndDate != nil {
		query = query.Where("occurred_at <= ?", *filters.EndDate)
	}

	return query
}

var allowedBusinessMetricSortFields = map[string]string{
	"occurred_at": "occurred_at",
	"occurredat":  "occurred_at",
	"created_at":  "created_at",
	"createdat":   "created_at",
	"event_type":  "event_type",
	"eventtype":   "event_type",
	"entity_type": "entity_type",
	"entitytype":  "entity_type",
	"user_id":     "user_id",
	"userid":      "user_id",
}

func applySorts(query *gorm.DB, sorts []shared.Sort) *gorm.DB {
	applied := false
	for _, sort := range sorts {
		col, ok := allowedBusinessMetricSortFields[strings.ToLower(strings.TrimSpace(sort.Field))]
		if !ok {
			continue
		}
		dir := "ASC"
		if sort.Direction == shared.SortDesc {
			dir = "DESC"
		}
		query = query.Order(col + " " + dir)
		applied = true
	}
	if !applied {
		return query.Order("occurred_at DESC")
	}
	return query
}

func mapToSchema(metric *business_metrics.Metric) *schema.BusinessMetric {
	var metadataJSON string
	if len(metric.Metadata) > 0 {
		bytes, _ := json.Marshal(metric.Metadata)
		metadataJSON = string(bytes)
	}

	return &schema.BusinessMetric{
		ID:         metric.ID,
		EventType:  string(metric.EventType),
		EntityID:   metric.EntityID,
		EntityType: string(metric.EntityType),
		UserID:     metric.UserID,
		Metadata:   metadataJSON,
		OccurredAt: metric.OccurredAt,
	}
}

func mapToDomain(record *schema.BusinessMetric) *business_metrics.Metric {
	var metadata map[string]string
	if record.Metadata != "" {
		_ = json.Unmarshal([]byte(record.Metadata), &metadata)
	}

	return &business_metrics.Metric{
		ID:         record.ID,
		EventType:  business_metrics.MetricEventType(record.EventType),
		EntityID:   record.EntityID,
		EntityType: business_metrics.EntityType(record.EntityType),
		UserID:     record.UserID,
		Metadata:   metadata,
		OccurredAt: record.OccurredAt,
		CreatedAt:  record.CreatedAt,
	}
}
