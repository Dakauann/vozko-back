package pipeline

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *PipelineHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	pl := workspace_domain.ResourceStages
	plRoutes := protected.PathPrefix("/pipelines").Subrouter()
	plRoutes.HandleFunc("", ac(pl, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	plRoutes.HandleFunc("", ac(pl, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	plRoutes.HandleFunc("/{id}", ac(pl, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	plRoutes.HandleFunc("/{id}", ac(pl, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPut)
	plRoutes.HandleFunc("/{id}", ac(pl, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
}
