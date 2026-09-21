package leadmemory

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *LeadMemoryHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	leads := workspace_domain.ResourceLeads

	protected.HandleFunc("/leads/{id}/memories",
		ac(leads, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/leads/{id}/memories",
		ac(leads, workspace_domain.ActionUpdate, h.Create)).Methods(http.MethodPost)

	protected.HandleFunc("/lead-memories/{id}",
		ac(leads, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPatch)
	protected.HandleFunc("/lead-memories/{id}",
		ac(leads, workspace_domain.ActionUpdate, h.Delete)).Methods(http.MethodDelete)
}
