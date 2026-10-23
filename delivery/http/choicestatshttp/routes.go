package choicestatshttp

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		return
	}
	read := workspace_domain.ActionRead
	protected.HandleFunc("/workflows/{id}/choice-stats", ac(workspace_domain.ResourceWorkflows, read, h.Workflow)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp-campaigns/{id}/choice-stats", ac(workspace_domain.ResourceWhatsAppCampaigns, read, h.WhatsAppCampaign)).Methods(http.MethodGet)
	protected.HandleFunc("/unofficial-whatsapp-campaigns/{id}/choice-stats", ac(workspace_domain.ResourceUnofficialWhatsAppCampaigns, read, h.UnofficialCampaign)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp-templates/{id}/choice-stats", ac(workspace_domain.ResourceWhatsAppTemplates, read, h.Template)).Methods(http.MethodGet)
}
