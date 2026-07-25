package telephony

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *TelephonyHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	tel := workspace_domain.ResourceAttendance
	protected.HandleFunc("/telephony/overview", ac(tel, workspace_domain.ActionRead, h.GetOverview)).Methods(http.MethodGet)
	protected.HandleFunc("/telephony/board", ac(tel, workspace_domain.ActionRead, h.GetBoard)).Methods(http.MethodGet)
}
