package mercadolivre

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

const (
	OAuthStartPath    = "/oauth/mercadolivre/start"
	OAuthCallbackPath = "/oauth/mercadolivre/callback"
	NotificationPath  = "/webhooks/mercadolivre/{token}"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		return
	}
	res := workspace_domain.ResourceMercadoLivreAccounts

	protected.HandleFunc(OAuthStartPath, ac(res, workspace_domain.ActionCreate, h.StartConnect)).Methods(http.MethodGet)

	ml := protected.PathPrefix("/mercadolivre").Subrouter()
	ml.HandleFunc("/accounts", ac(res, workspace_domain.ActionRead, h.ListAccounts)).Methods(http.MethodGet)
	ml.HandleFunc("/accounts/{id}", ac(res, workspace_domain.ActionRead, h.GetAccount)).Methods(http.MethodGet)
	ml.HandleFunc("/accounts/{id}", ac(res, workspace_domain.ActionUpdate, h.UpdateAccount)).Methods(http.MethodPatch)
	ml.HandleFunc("/accounts/{id}", ac(res, workspace_domain.ActionDelete, h.DisconnectAccount)).Methods(http.MethodDelete)
	ml.HandleFunc("/accounts/{id}/health-check", ac(res, workspace_domain.ActionUpdate, h.HealthCheck)).Methods(http.MethodPost)
}

func RegisterPublicRoutes(public *mux.Router, h *Handler, notifications *NotificationHandler) {
	if h != nil {
		public.HandleFunc(OAuthCallbackPath, h.HandleCallback).Methods(http.MethodGet)
	}
	if notifications != nil {
		public.HandleFunc(NotificationPath, notifications.Handle).Methods(http.MethodPost)
	}
}
