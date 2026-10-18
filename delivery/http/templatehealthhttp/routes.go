package templatehealthhttp

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
	protected.HandleFunc("/whatsapp/templates/{id}/health", ac(workspace_domain.ResourceWhatsAppTemplates, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
}
