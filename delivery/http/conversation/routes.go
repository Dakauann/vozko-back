package conversation

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *ConversationHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	cv := workspace_domain.ResourceConversations

	protected.HandleFunc("/conversations/inbox/search", ac(cv, workspace_domain.ActionRead, h.SearchInbox)).Methods(http.MethodGet)

	protected.HandleFunc("/conversations/{entryType}/{entryId}/messages/search", ac(cv, workspace_domain.ActionRead, h.SearchMessages)).Methods(http.MethodGet)

	protected.HandleFunc("/conversations/{entryType}/{entryId}/messages", ac(cv, workspace_domain.ActionUpdate, h.SendMessage)).Methods(http.MethodPost)
	protected.HandleFunc("/conversations/{entryType}/{entryId}/call-permission-request", ac(cv, workspace_domain.ActionCall, h.RequestCallPermission)).Methods(http.MethodPost)
	protected.HandleFunc("/conversations/{entryType}/{entryId}/call-permission", ac(cv, workspace_domain.ActionRead, h.GetCallPermission)).Methods(http.MethodGet)

	protected.HandleFunc("/conversations/{entryType}/{entryId}/media", ac(cv, workspace_domain.ActionUpdate, h.UploadMedia)).Methods(http.MethodPost)

	protected.HandleFunc("/conversations/{entryType}/{entryId}/media/{mediaId}", ac(cv, workspace_domain.ActionRead, h.GetMedia)).Methods(http.MethodGet)

	protected.HandleFunc("/conversations/{entryType}/{entryId}/events", ac(cv, workspace_domain.ActionRead, h.ListConversationEvents)).Methods(http.MethodGet)

	protected.HandleFunc("/conversations/{entryType}/{entryId}/reopen-window", ac(cv, workspace_domain.ActionUpdate, h.ReopenWindow)).Methods(http.MethodPost)
	// Channel-neutral automation toggle. The only previous way to set this was
	// a PATCH on a WhatsApp CAMPAIGN entry, which no other channel has.
	protected.HandleFunc("/conversations/{entryType}/{entryId}/automation", ac(cv, workspace_domain.ActionUpdate, h.SetAutomation)).Methods(http.MethodPatch)
}
