package scheduledmessage

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *ScheduledMessageHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	cv := workspace_domain.ResourceConversations

	protected.HandleFunc("/conversations/{entryType}/{entryId}/scheduled-messages",
		ac(cv, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/conversations/{entryType}/{entryId}/scheduled-messages",
		ac(cv, workspace_domain.ActionSend, h.Create)).Methods(http.MethodPost)

	protected.HandleFunc("/scheduled-messages",
		ac(cv, workspace_domain.ActionRead, h.ListWorkspace)).Methods(http.MethodGet)
	protected.HandleFunc("/scheduled-messages/{id}",
		ac(cv, workspace_domain.ActionSend, h.Reschedule)).Methods(http.MethodPatch)
	protected.HandleFunc("/scheduled-messages/{id}",
		ac(cv, workspace_domain.ActionSend, h.Cancel)).Methods(http.MethodDelete)
}
