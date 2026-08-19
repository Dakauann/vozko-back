package whatsappoutreach

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

// RegisterProtectedRoutes wires cold outbound on the official channel.
//
// ActionSend on the TEMPLATES resource, matching the unofficial channel's split
// between answering and initiating — and sharper here, because on this channel
// initiating spends the workspace's balance. An attendant with full attendance
// rights does not get it by default.
//
// The quote is ActionRead: it reports a price, it does not spend one. Gating it
// behind ActionSend would mean an operator could not be shown the cost of an
// action they are about to be asked to approve.
func RegisterProtectedRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	// A nil handler means the channel is not wired; register nothing rather than
	// routes whose methods would nil-panic on the first request.
	if h == nil {
		return
	}

	res := workspace_domain.ResourceWhatsAppTemplates
	r := protected.PathPrefix("/whatsapp/outreach").Subrouter()

	r.HandleFunc("/conversations", ac(res, workspace_domain.ActionSend, h.StartConversation)).Methods(http.MethodPost)
	r.HandleFunc("/quote", ac(res, workspace_domain.ActionRead, h.Quote)).Methods(http.MethodGet)
}
