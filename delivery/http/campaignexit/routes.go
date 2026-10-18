package campaignexithttp

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(protected *mux.Router, h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc) {
	protected.HandleFunc("/whatsapp/campaigns/exit-audience", ac(workspace_domain.ResourceWhatsAppCampaigns, workspace_domain.ActionCreate, h.AudienceExits)).Methods(http.MethodPost)
}
