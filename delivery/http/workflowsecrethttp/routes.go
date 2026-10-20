package workflowsecrethttp

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	res := workspace_domain.ResourceWorkflows
	protected.HandleFunc("/workflow-secrets", ac(res, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/workflow-secrets/{name}", ac(res, workspace_domain.ActionUpdate, h.Put)).Methods(http.MethodPut)
	protected.HandleFunc("/workflow-secrets/{name}", ac(res, workspace_domain.ActionUpdate, h.Delete)).Methods(http.MethodDelete)
}
