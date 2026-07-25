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
	attRoutes.HandleFunc("/overview", ac(att, workspace_domain.ActionRead, h.GetOverview)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/stats", ac(att, workspace_domain.ActionRead, h.GetAttendanceStats)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/windows", ac(att, workspace_domain.ActionRead, h.GetWindowStats)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/response-times", ac(att, workspace_domain.ActionRead, h.GetResponseTimeDistribution)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/ai-stats", ac(att, workspace_domain.ActionRead, h.GetAIAgentStats)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/frt", ac(att, workspace_domain.ActionRead, h.GetFRTStats)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/queue-stats", ac(att, workspace_domain.ActionRead, h.GetQueueStats)).Methods(http.MethodGet)
	attRoutes.HandleFunc("/occupancy", ac(att, workspace_domain.ActionRead, h.GetOccupancy)).Methods(http.MethodGet)
}
