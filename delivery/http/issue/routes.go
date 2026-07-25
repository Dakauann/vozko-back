package issue

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *IssueHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	is := workspace_domain.ResourceIssues
	protected.HandleFunc("/issues", ac(is, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/issues", ac(is, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	protected.HandleFunc("/issues/{id}", ac(is, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	protected.HandleFunc("/issues/{id}/close", ac(is, workspace_domain.ActionUpdate, h.Close)).Methods(http.MethodPatch)
	protected.HandleFunc("/issues/{id}/responses", ac(is, workspace_domain.ActionRead, h.ListResponses)).Methods(http.MethodGet)
	protected.HandleFunc("/issues/{id}/responses", ac(is, workspace_domain.ActionUpdate, h.CreateResponse)).Methods(http.MethodPost)
}

func RegisterAdminRoutes(admin *mux.Router, h *IssueHandler) {
	admin.HandleFunc("/admin/issues", h.ListAll).Methods(http.MethodGet)
	admin.HandleFunc("/admin/issues/{id}", h.Get).Methods(http.MethodGet)
	admin.HandleFunc("/admin/issues/{id}/status", h.UpdateStatus).Methods(http.MethodPatch)
	admin.HandleFunc("/admin/issues/{id}/responses", h.ListResponses).Methods(http.MethodGet)
	admin.HandleFunc("/admin/issues/{id}/responses", h.CreateResponse).Methods(http.MethodPost)
}
