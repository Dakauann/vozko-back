package messageshortcut

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *MessageShortcutHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	ms := workspace_domain.ResourceMessageShortcuts
	protected.HandleFunc("/message-shortcuts", ac(ms, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/message-shortcuts", ac(ms, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	protected.HandleFunc("/message-shortcuts/by-shortcut/{shortcut}", ac(ms, workspace_domain.ActionRead, h.GetByShortcut)).Methods(http.MethodGet)
	protected.HandleFunc("/message-shortcuts/{id}", ac(ms, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPut)
	protected.HandleFunc("/message-shortcuts/{id}", ac(ms, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
}
