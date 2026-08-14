package leadmemory

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

// RegisterRoutes wires the lead-memory endpoints.
//
// No new workspace resource: memories ARE lead data, so they ride the existing
// `leads` permissions ("Visualizar leads" to read, "Editar dados de leads" to
// write). Inventing a `lead_memories` resource would mean every existing role
// silently losing the ability the day this deploys.
//
// Lead-scoped routes nest under /leads/{id} like the rest of the lead surface;
// the by-id routes are flat, matching every other resource.
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
