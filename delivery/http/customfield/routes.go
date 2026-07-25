package customfield

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *CustomFieldHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	cf := workspace_domain.ResourceConversations
	cfRoutes := protected.PathPrefix("/custom-fields").Subrouter()
	cfRoutes.HandleFunc("", ac(cf, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	cfRoutes.HandleFunc("", ac(cf, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	cfRoutes.HandleFunc("/{id}", ac(cf, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	cfRoutes.HandleFunc("/{id}", ac(cf, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPatch)
	cfRoutes.HandleFunc("/{id}", ac(cf, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
}
