package buildersession

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *BuilderSessionHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	wf := workspace_domain.ResourceWorkflows
	protected.HandleFunc("/workflows/{id}/builder-sessions", ac(wf, workspace_domain.ActionRead, h.ListByWorkflow)).Methods(http.MethodGet)
	protected.HandleFunc("/builder-sessions", ac(wf, workspace_domain.ActionRead, h.ListByWorkspace)).Methods(http.MethodGet)
	protected.HandleFunc("/builder-sessions/{sessionId}", ac(wf, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
}
