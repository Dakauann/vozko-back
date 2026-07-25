package savedview

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *SavedViewHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	sv := workspace_domain.ResourceConversations
	svRoutes := protected.PathPrefix("/saved-views").Subrouter()
	svRoutes.HandleFunc("", ac(sv, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	svRoutes.HandleFunc("", ac(sv, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	svRoutes.HandleFunc("/{id}", ac(sv, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPut)
	svRoutes.HandleFunc("/{id}", ac(sv, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
	svRoutes.HandleFunc("/{id}/default", ac(sv, workspace_domain.ActionUpdate, h.SetDefault)).Methods(http.MethodPut)
}
