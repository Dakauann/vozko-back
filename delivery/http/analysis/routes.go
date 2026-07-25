package analysis

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *AnalysisHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	an := workspace_domain.ResourceAnalysis
	protected.HandleFunc("/analysis", ac(an, workspace_domain.ActionRead, h.ListForUser)).Methods(http.MethodGet)
	protected.HandleFunc("/analysis/stats", ac(an, workspace_domain.ActionRead, h.GetStatsForUser)).Methods(http.MethodGet)
	protected.HandleFunc("/analysis/entry/{entryId}", ac(an, workspace_domain.ActionRead, h.GetByEntry)).Methods(http.MethodGet)
}

func RegisterAdminRoutes(adminRoutes *mux.Router, h *AnalysisHandler) {
	adminRoutes.HandleFunc("/admin/analysis", h.List).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/analysis/stats", h.GetStats).Methods(http.MethodGet)
}
