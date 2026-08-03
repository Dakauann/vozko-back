package telegram

import (
	"net/http"

	"github.com/gorilla/mux"

	tgdomain "vozko/domain/telegram"
	workspace_domain "vozko/domain/workspace"
)

// RegisterProtectedRoutes wires the authenticated Telegram routes.
//
// Read/write splits along the RBAC actions used elsewhere: connecting is
// ActionCreate, reading is ActionRead, editing config and minting links is
// ActionUpdate, disconnecting is ActionDelete.
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

	res := workspace_domain.ResourceTelegramAccounts
	tg := protected.PathPrefix("/telegram").Subrouter()

	tg.HandleFunc("/accounts", ac(res, workspace_domain.ActionCreate, h.ConnectAccount)).Methods(http.MethodPost)
	tg.HandleFunc("/accounts", ac(res, workspace_domain.ActionRead, h.ListAccounts)).Methods(http.MethodGet)
	tg.HandleFunc("/accounts/{id}", ac(res, workspace_domain.ActionRead, h.GetAccount)).Methods(http.MethodGet)
	tg.HandleFunc("/accounts/{id}", ac(res, workspace_domain.ActionUpdate, h.UpdateAccount)).Methods(http.MethodPut)
	tg.HandleFunc("/accounts/{id}", ac(res, workspace_domain.ActionDelete, h.DisconnectAccount)).Methods(http.MethodDelete)

	// Re-registering the webhook is a repair action, not a new connection, so it
	// is an update on an existing account.
	tg.HandleFunc("/accounts/{id}/webhook", ac(res, workspace_domain.ActionUpdate, h.ReregisterWebhook)).Methods(http.MethodPost)

	// Deep links: the channel's substitute for cold outbound.
	tg.HandleFunc("/accounts/{id}/deep-links", ac(res, workspace_domain.ActionRead, h.ListDeepLinks)).Methods(http.MethodGet)
	tg.HandleFunc("/accounts/{id}/deep-links", ac(res, workspace_domain.ActionUpdate, h.CreateDeepLink)).Methods(http.MethodPost)
	tg.HandleFunc("/accounts/{id}/deep-links/{token}", ac(res, workspace_domain.ActionUpdate, h.DeleteDeepLink)).Methods(http.MethodDelete)
}

// RegisterPublicRoutes wires the unauthenticated webhook endpoint.
//
// It is unauthenticated by necessity, Telegram calls it, and protected instead
// by the per-account secret token echoed in X-Telegram-Bot-Api-Secret-Token.
// Note the path carries OUR account uuid, never the bot token: an Update object
// contains no bot identity, so tenancy has to come from the URL, and a token in
// a URL would leak through proxy logs and Referer headers.
func RegisterPublicRoutes(public *mux.Router, wh *WebhookHandler) {
	if wh == nil {
		return
	}
	public.HandleFunc(tgdomain.WebhookPathTemplate, wh.Handle).Methods(http.MethodPost)
}
