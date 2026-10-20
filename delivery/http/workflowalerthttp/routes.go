package workflowalerthttp

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
	protected.HandleFunc("/workflows/{id}/alerts", ac(res, workspace_domain.ActionRead, h.ListForWorkflow)).Methods(http.MethodGet)
	protected.HandleFunc("/workflow-alerts", ac(res, workspace_domain.ActionRead, h.ListOpen)).Methods(http.MethodGet)
}
