package conversationnotehttp

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
	cv := workspace_domain.ResourceConversations

	protected.HandleFunc("/conversations/{entryType}/{entryId}/notes",
		ac(cv, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/conversations/{entryType}/{entryId}/notes",
		ac(cv, workspace_domain.ActionRead, h.Create)).Methods(http.MethodPost)
	protected.HandleFunc("/conversations/{entryType}/{entryId}/notes/images",
		ac(cv, workspace_domain.ActionRead, h.AttachImage)).Methods(http.MethodPost)
	protected.HandleFunc("/conversation-notes/{id}",
		ac(cv, workspace_domain.ActionRead, h.Delete)).Methods(http.MethodDelete)
}
