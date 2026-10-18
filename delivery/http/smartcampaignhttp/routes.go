package smartcampaignhttp

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
	campaigns := workspace_domain.ResourceWhatsAppCampaigns
	protected.HandleFunc("/whatsapp/campaign-waves/defaults", ac(campaigns, workspace_domain.ActionCreate, h.Defaults)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp/campaign-waves/projection", ac(campaigns, workspace_domain.ActionCreate, h.Projection)).Methods(http.MethodPost)
	protected.HandleFunc("/whatsapp/campaigns/{id}/smart", ac(campaigns, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp/campaigns/{id}/smart", ac(campaigns, workspace_domain.ActionUpdate, h.Save)).Methods(http.MethodPut)
	protected.HandleFunc("/whatsapp/campaigns/{id}/smart", ac(campaigns, workspace_domain.ActionUpdate, h.Remove)).Methods(http.MethodDelete)
}
