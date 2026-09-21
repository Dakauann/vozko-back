package telegram

import (
	"net/http"

	"github.com/gorilla/mux"

	tgdomain "vozko/domain/telegram"
	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
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

	tg.HandleFunc("/accounts/{id}/webhook", ac(res, workspace_domain.ActionUpdate, h.ReregisterWebhook)).Methods(http.MethodPost)

	tg.HandleFunc("/accounts/{id}/deep-links", ac(res, workspace_domain.ActionRead, h.ListDeepLinks)).Methods(http.MethodGet)
	tg.HandleFunc("/accounts/{id}/deep-links", ac(res, workspace_domain.ActionUpdate, h.CreateDeepLink)).Methods(http.MethodPost)
	tg.HandleFunc("/accounts/{id}/deep-links/{token}", ac(res, workspace_domain.ActionUpdate, h.DeleteDeepLink)).Methods(http.MethodDelete)
}

func RegisterPublicRoutes(public *mux.Router, wh *WebhookHandler) {
	if wh == nil {
		return
	}
	public.HandleFunc(tgdomain.WebhookPathTemplate, wh.Handle).Methods(http.MethodPost)
}
