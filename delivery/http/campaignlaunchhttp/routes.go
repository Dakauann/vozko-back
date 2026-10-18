package campaignlaunchhttp

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
	protected.HandleFunc("/whatsapp/campaigns/{id}/launch-check", ac(workspace_domain.ResourceWhatsAppCampaigns, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
}
