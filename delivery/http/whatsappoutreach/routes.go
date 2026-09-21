package whatsappoutreach

import (
	"net/http"

	"github.com/gorilla/mux"

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

	res := workspace_domain.ResourceWhatsAppTemplates
	r := protected.PathPrefix("/whatsapp/outreach").Subrouter()

	r.HandleFunc("/conversations", ac(res, workspace_domain.ActionSend, h.StartConversation)).Methods(http.MethodPost)
	r.HandleFunc("/quote", ac(res, workspace_domain.ActionRead, h.Quote)).Methods(http.MethodGet)
}
