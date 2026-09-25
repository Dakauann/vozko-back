package attendance

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *AttendanceHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	att := workspace_domain.ResourceAttendance
	attRoutes := protected.PathPrefix("/attendance").Subrouter()
	attRoutes.HandleFunc("/overview/{section}", ac(att, workspace_domain.ActionRead, h.GetOverviewSection)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/stats", ac(att, workspace_domain.ActionRead, h.GetAttendanceStats)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/windows", ac(att, workspace_domain.ActionRead, h.GetWindowStats)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/response-times", ac(att, workspace_domain.ActionRead, h.GetResponseTimeDistribution)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/ai-stats", ac(att, workspace_domain.ActionRead, h.GetAIAgentStats)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/frt", ac(att, workspace_domain.ActionRead, h.GetFRTStats)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/queue-stats", ac(att, workspace_domain.ActionRead, h.GetQueueStats)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/occupancy", ac(att, workspace_domain.ActionRead, h.GetOccupancy)).Methods(http.MethodGet)

	targets := workspace_domain.ResourceAttendanceTargets
	attRoutes.HandleFunc("/metrics", ac(att, workspace_domain.ActionRead, h.GetTargetableMetrics)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/targets", ac(targets, workspace_domain.ActionRead, h.ListTargets)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/targets", ac(targets, workspace_domain.ActionUpdate, h.UpsertTarget)).Methods(http.MethodPut)
	attRoutes.HandleFunc("/targets/{id}", ac(targets, workspace_domain.ActionDelete, h.DeleteTarget)).Methods(http.MethodDelete)
}
