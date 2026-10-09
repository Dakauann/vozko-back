package siptrunk

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
	res := workspace_domain.ResourceSIPTrunks
	trunks := protected.PathPrefix("/sip-trunks").Subrouter()
	trunks.HandleFunc("", ac(res, workspace_domain.ActionCreate, h.CreateTrunk)).Methods(http.MethodPost)
	trunks.HandleFunc("", ac(res, workspace_domain.ActionRead, h.ListTrunks)).Methods(http.MethodGet)
	trunks.HandleFunc("/{id}", ac(res, workspace_domain.ActionRead, h.GetTrunk)).Methods(http.MethodGet)
	trunks.HandleFunc("/{id}", ac(res, workspace_domain.ActionUpdate, h.UpdateTrunk)).Methods(http.MethodPut)
	trunks.HandleFunc("/{id}", ac(res, workspace_domain.ActionDelete, h.DeleteTrunk)).Methods(http.MethodDelete)
	trunks.HandleFunc("/{id}/calls", ac(res, workspace_domain.ActionRead, h.ListCalls)).Methods(http.MethodGet)
	trunks.HandleFunc("/{id}/calls/{callId}", ac(res, workspace_domain.ActionCall, h.HangupCall)).Methods(http.MethodDelete)
	protected.HandleFunc("/dial-targets", ac(res, workspace_domain.ActionRead, h.DialTargets)).Methods(http.MethodGet)
}
