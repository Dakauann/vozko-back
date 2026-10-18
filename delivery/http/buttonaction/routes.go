package buttonactionhttp

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(protected *mux.Router, h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc) {
	templates, campaigns := workspace_domain.ResourceWhatsAppTemplates, workspace_domain.ResourceWhatsAppCampaigns
	protected.HandleFunc("/whatsapp/templates/{id}/button-actions", ac(templates, workspace_domain.ActionRead, h.GetTemplateActions)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp/templates/{id}/button-actions", ac(templates, workspace_domain.ActionUpdate, h.SaveTemplateActions)).Methods(http.MethodPut)
	protected.HandleFunc("/whatsapp/templates/{id}/exit-verdict", ac(templates, workspace_domain.ActionRead, h.GetExitVerdict)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp/templates/exit-verdict", ac(templates, workspace_domain.ActionCreate, h.DraftExitVerdict)).Methods(http.MethodPost)
	protected.HandleFunc("/whatsapp/campaigns/{id}/button-routes", ac(campaigns, workspace_domain.ActionRead, h.GetCampaignRoutes)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp/campaigns/{id}/button-routes", ac(campaigns, workspace_domain.ActionCreate, h.SaveCampaignRoutes)).Methods(http.MethodPut)
}
