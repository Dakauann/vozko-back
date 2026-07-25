package analytics

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterAdminRoutes(admin *mux.Router, h *AnalyticsHandler) {
	analyticsRoutes := admin.PathPrefix("/admin/analytics").Subrouter()
	analyticsRoutes.HandleFunc("/overview", h.GetAdminOverview).Methods(http.MethodGet)
	analyticsRoutes.HandleFunc("/profit", h.GetProfitReport).Methods(http.MethodGet)
	analyticsRoutes.HandleFunc("/profit/calls", h.GetCallAnalytics).Methods(http.MethodGet)
	analyticsRoutes.HandleFunc("/contractions", h.GetPlanContractions).Methods(http.MethodGet)
}
