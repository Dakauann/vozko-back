package businessmetrics

import (
	"encoding/json"
	"net/http"
	"strconv"

	"vozko/delivery/http/response"
	businessmetricsdomain "vozko/domain/business_metrics"
)

type BusinessMetricsHandler struct {
	listMetrics          businessmetricsdomain.ListMetricsUseCase
	getMetricsStats      businessmetricsdomain.GetMetricsStatsUseCase
	getMetricsTimeSeries businessmetricsdomain.GetMetricsTimeSeriesUseCase
}

func NewBusinessMetricsHandler(
	listMetrics businessmetricsdomain.ListMetricsUseCase,
	getMetricsStats businessmetricsdomain.GetMetricsStatsUseCase,
	getMetricsTimeSeries businessmetricsdomain.GetMetricsTimeSeriesUseCase,
) *BusinessMetricsHandler {
	return &BusinessMetricsHandler{
		listMetrics:          listMetrics,
		getMetricsStats:      getMetricsStats,
		getMetricsTimeSeries: getMetricsTimeSeries,
	}
}

func (h *BusinessMetricsHandler) ListMetrics(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	page := 1
	if p := query.Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	pageSize := 50
	if ps := query.Get("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}

	var eventTypes []businessmetricsdomain.MetricEventType
	if et := query.Get("event_types"); et != "" {
		var eventTypesStr []string
		if err := json.Unmarshal([]byte(et), &eventTypesStr); err == nil {
			for _, ets := range eventTypesStr {
				eventTypes = append(eventTypes, businessmetricsdomain.MetricEventType(ets))
			}
		} else {
			eventTypes = append(eventTypes, businessmetricsdomain.MetricEventType(et))
		}
	}

	if et := query.Get("event_type"); et != "" {
		eventTypes = append(eventTypes, businessmetricsdomain.MetricEventType(et))
	}

	var entityType *businessmetricsdomain.EntityType
	if et := query.Get("entity_type"); et != "" {
		entityTypeVal := businessmetricsdomain.EntityType(et)
		entityType = &entityTypeVal
	}

	var userID *string
	if uid := query.Get("user_id"); uid != "" {
		userID = &uid
	}

	var startDate, endDate *string
	if sd := query.Get("start_date"); sd != "" {
		startDate = &sd
	}
	if ed := query.Get("end_date"); ed != "" {
		endDate = &ed
	}

	sortBy := query.Get("sort_by")
	if sortBy == "" {
		sortBy = "occurred_at"
	}
	sortOrder := query.Get("sort_order")
	if sortOrder == "" {
		sortOrder = "desc"
	}

	input := businessmetricsdomain.ListMetricsInput{
		EventTypes: eventTypes,
		EntityType: entityType,
		UserID:     userID,
		StartDate:  startDate,
		EndDate:    endDate,
		Page:       page,
		PageSize:   pageSize,
		SortBy:     sortBy,
		SortOrder:  sortOrder,
	}

	result, err := h.listMetrics.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to retrieve metrics", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toListMetricsResponse(result))
}

func (h *BusinessMetricsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	var eventTypes []businessmetricsdomain.MetricEventType
	if et := query.Get("event_types"); et != "" {
		var eventTypesStr []string
		if err := json.Unmarshal([]byte(et), &eventTypesStr); err == nil {
			for _, ets := range eventTypesStr {
				eventTypes = append(eventTypes, businessmetricsdomain.MetricEventType(ets))
			}
		} else {
			eventTypes = append(eventTypes, businessmetricsdomain.MetricEventType(et))
		}
	}

	if et := query.Get("event_type"); et != "" {
		eventTypes = append(eventTypes, businessmetricsdomain.MetricEventType(et))
	}

	var startDate, endDate *string
	if sd := query.Get("start_date"); sd != "" {
		startDate = &sd
	}
	if ed := query.Get("end_date"); ed != "" {
		endDate = &ed
	}

	input := businessmetricsdomain.GetMetricsStatsInput{
		EventTypes: eventTypes,
		StartDate:  startDate,
		EndDate:    endDate,
	}

	result, err := h.getMetricsStats.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to retrieve statistics", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toMetricsStatsResponse(result))
}

func (h *BusinessMetricsHandler) GetTimeSeries(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	var eventTypes []businessmetricsdomain.MetricEventType
	if et := query.Get("event_types"); et != "" {
		var eventTypesStr []string
		if err := json.Unmarshal([]byte(et), &eventTypesStr); err == nil {
			for _, ets := range eventTypesStr {
				eventTypes = append(eventTypes, businessmetricsdomain.MetricEventType(ets))
			}
		} else {
			eventTypes = append(eventTypes, businessmetricsdomain.MetricEventType(et))
		}
	}

	if et := query.Get("event_type"); et != "" {
		eventTypes = append(eventTypes, businessmetricsdomain.MetricEventType(et))
	}

	interval := businessmetricsdomain.IntervalHour
	if i := query.Get("interval"); i != "" {
		interval = businessmetricsdomain.TimeSeriesInterval(i)
	}

	var startDate, endDate *string
	if sd := query.Get("start_date"); sd != "" {
		startDate = &sd
	}
	if ed := query.Get("end_date"); ed != "" {
		endDate = &ed
	}

	input := businessmetricsdomain.GetMetricsTimeSeriesInput{
		EventTypes: eventTypes,
		Interval:   interval,
		StartDate:  startDate,
		EndDate:    endDate,
	}

	result, err := h.getMetricsTimeSeries.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toMetricsTimeSeriesResponse(result))
}
