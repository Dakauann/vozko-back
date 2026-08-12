package scheduledmessage

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

// RegisterRoutes wires the scheduled-message endpoints.
//
// No new workspace resource: scheduling IS sending, just later, so it is gated
// on the permissions that already exist. Inventing a `scheduled_messages`
// resource would mean every existing role silently losing the ability the day
// this deploys.
//
// The entry-scoped routes are nested under the conversation because that is
// where entryType/entryId are validated and the caller's access to the
// conversation is checked; the by-id routes are flat, matching every other
// resource.
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
