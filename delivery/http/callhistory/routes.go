package callhistory

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(protected *mux.Router, h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc) {
	if h == nil {
		return
	}
	res := workspace_domain.ResourceCallHistory
	protected.HandleFunc("/calls", ac(res, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/calls/{callId}", ac(res, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
}
