package metaembeddedsignup

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *MetaEmbeddedSignupHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	bp := workspace_domain.ResourceBusinessPhones
	protected.HandleFunc("/whatsapp/onboarding-config", ac(bp, workspace_domain.ActionRead, h.GetOnboardingConfig)).Methods(http.MethodGet)

	protected.HandleFunc("/oauth/meta/embedded", ac(bp, workspace_domain.ActionCreate, func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodGet:
			h.ServeEmbeddedSignupPage(w, req)
		case http.MethodPost:
			h.HandleEmbeddedSignupCallback(w, req)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})).Methods(http.MethodGet, http.MethodPost)

	protected.HandleFunc("/onboard/360dialog/retry", ac(bp, workspace_domain.ActionCreate, func(w http.ResponseWriter, req *http.Request) {
		h.HandleDialog360Retry(w, req)
	})).Methods(http.MethodPost)
}

func RegisterPublicRoutes(public *mux.Router, h *MetaEmbeddedSignupHandler) {
	public.HandleFunc("/webhooks/360dialog/partner", h.HandleDialog360PartnerWebhook).Methods(http.MethodPost)
}
