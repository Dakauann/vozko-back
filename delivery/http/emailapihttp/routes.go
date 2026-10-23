package emailapihttp

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/apikeys"
	"vozko/domain/apikey"
	workspace_domain "vozko/domain/workspace"
)

const KeyRoutesPrefix = "/email/transactional/messages"

func RegisterKeyRoutes(root *mux.Router, h *Handler, auth *apikeys.Authenticator) {
	if h == nil || auth == nil {
		return
	}
	root.HandleFunc(KeyRoutesPrefix, auth.Require(apikey.ScopeEmailSend, h.Send)).Methods(http.MethodPost)
	root.HandleFunc(KeyRoutesPrefix, auth.Require(apikey.ScopeEmailRead, h.APIList)).Methods(http.MethodGet)
	root.HandleFunc(KeyRoutesPrefix+"/{id}", auth.Require(apikey.ScopeEmailRead, h.APIDetail)).Methods(http.MethodGet)
}

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		return
	}
	res := workspace_domain.ResourceEmailMarketing
	read, configure := workspace_domain.ActionRead, workspace_domain.ActionConfigure
	protected.HandleFunc("/email/transactional/log", ac(res, read, h.LogList)).Methods(http.MethodGet)
	protected.HandleFunc("/email/transactional/log/{id}", ac(res, read, h.LogDetail)).Methods(http.MethodGet)
	protected.HandleFunc("/email/transactional/webhooks", ac(res, read, h.ListWebhooks)).Methods(http.MethodGet)
	protected.HandleFunc("/email/transactional/webhooks", ac(res, configure, h.CreateWebhook)).Methods(http.MethodPost)
	protected.HandleFunc("/email/transactional/webhooks/{id}", ac(res, configure, h.UpdateWebhook)).Methods(http.MethodPatch)
	protected.HandleFunc("/email/transactional/webhooks/{id}", ac(res, configure, h.DeleteWebhook)).Methods(http.MethodDelete)
	protected.HandleFunc("/email/transactional/webhooks/{id}/secret", ac(res, configure, h.RotateWebhookSecret)).Methods(http.MethodPost)
	protected.HandleFunc("/email/transactional/webhooks/{id}/deliveries", ac(res, read, h.WebhookDeliveries)).Methods(http.MethodGet)
	protected.HandleFunc("/email/transactional/webhooks/{id}/deliveries/{deliveryId}/retry", ac(res, configure, h.RetryWebhookDelivery)).Methods(http.MethodPost)
}
