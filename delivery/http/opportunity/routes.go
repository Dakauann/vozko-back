package opportunity

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *OpportunityHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	op := workspace_domain.ResourceConversations
	opRoutes := protected.PathPrefix("/opportunities").Subrouter()
	opRoutes.HandleFunc("", ac(op, workspace_domain.ActionRead, h.ListByPipeline)).Methods(http.MethodGet)
	opRoutes.HandleFunc("", ac(op, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	opRoutes.HandleFunc("/export", ac(op, workspace_domain.ActionRead, h.Export)).Methods(http.MethodGet)
	opRoutes.HandleFunc("/import", ac(op, workspace_domain.ActionCreate, h.Import)).Methods(http.MethodPost)
	opRoutes.HandleFunc("/for-entry", ac(op, workspace_domain.ActionRead, h.ListForEntry)).Methods(http.MethodGet)
	opRoutes.HandleFunc("/{id}", ac(op, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	opRoutes.HandleFunc("/{id}", ac(op, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPatch)
	opRoutes.HandleFunc("/{id}", ac(op, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
	opRoutes.HandleFunc("/{id}/move", ac(op, workspace_domain.ActionUpdate, h.MoveStage)).Methods(http.MethodPost)
	opRoutes.HandleFunc("/{id}/conversations", ac(op, workspace_domain.ActionRead, h.ListConversations)).Methods(http.MethodGet)
	opRoutes.HandleFunc("/{id}/conversations", ac(op, workspace_domain.ActionUpdate, h.LinkConversation)).Methods(http.MethodPost)
	opRoutes.HandleFunc("/{id}/conversations", ac(op, workspace_domain.ActionUpdate, h.UnlinkConversation)).Methods(http.MethodDelete)
}
