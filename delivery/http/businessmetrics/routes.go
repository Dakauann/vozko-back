package businessmetrics

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterAdminRoutes(admin *mux.Router, h *BusinessMetricsHandler) {
	admin.HandleFunc("/admin/business-metrics", h.ListMetrics).Methods(http.MethodGet)
	admin.HandleFunc("/admin/business-metrics/stats", h.GetStats).Methods(http.MethodGet)
	admin.HandleFunc("/admin/business-metrics/timeseries", h.GetTimeSeries).Methods(http.MethodGet)
}
