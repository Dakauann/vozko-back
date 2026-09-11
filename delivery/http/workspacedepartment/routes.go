package workspacedepartment

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *WorkspaceDepartmentHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	dp := workspace_domain.ResourceDepartments

	protected.HandleFunc("/departments", h.List).Methods(http.MethodGet)
	protected.HandleFunc("/departments", ac(dp, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	// Before /departments/{id}: mux matches in registration order, and the
	// parameterised route would otherwise swallow "scope" as an id.
	//
	// Deliberately ungated. It describes the CALLER's own visibility and
	// nothing else, so the member who most needs it, the one seeing an empty
	// screen because they are in no department, is exactly the one with no
	// departments:read to gate on.
	protected.HandleFunc("/departments/scope", h.MyScope).Methods(http.MethodGet)
	protected.HandleFunc("/departments/{id}", ac(dp, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	protected.HandleFunc("/departments/{id}", ac(dp, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPut)
	protected.HandleFunc("/departments/{id}", ac(dp, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
	protected.HandleFunc("/departments/{id}/members", ac(dp, workspace_domain.ActionRead, h.ListMembers)).Methods(http.MethodGet)
	protected.HandleFunc("/departments/{id}/members", ac(dp, workspace_domain.ActionUpdate, h.AddMember)).Methods(http.MethodPost)
	protected.HandleFunc("/departments/{id}/members/{memberId}", ac(dp, workspace_domain.ActionUpdate, h.RemoveMember)).Methods(http.MethodDelete)
}
