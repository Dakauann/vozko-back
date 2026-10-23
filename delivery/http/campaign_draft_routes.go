package http

import (
	"github.com/gorilla/mux"

	"vozko/delivery/http/campaigndrafthttp"
	workspace_domain "vozko/domain/workspace"
)

type CampaignDraftRoutes struct {
	WhatsApp   *campaigndrafthttp.Handler
	Unofficial *campaigndrafthttp.Handler
}

func (r *router) setupCampaignDraftRoutes(protected *mux.Router) {
	campaigndrafthttp.RegisterProtectedRoutes(protected, r.campaignDrafts.WhatsApp, r.ac, workspace_domain.ResourceWhatsAppCampaigns)
	campaigndrafthttp.RegisterProtectedRoutes(protected, r.campaignDrafts.Unofficial, r.ac, workspace_domain.ResourceUnofficialWhatsAppCampaigns)
}
