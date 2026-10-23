package workflowrunexporthttp

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
	protected.HandleFunc("/workflow-runs/{runId}/export", ac(res, workspace_domain.ActionRead, h.ExportRun)).Methods(http.MethodGet)
	protected.HandleFunc("/workflows/{id}/runs/export", ac(res, workspace_domain.ActionRead, h.ExportRuns)).Methods(http.MethodGet)
}
