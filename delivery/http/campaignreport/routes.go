package campaignreport

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
	read := workspace_domain.ActionRead
	section := ac(workspace_domain.ResourceAttendance, read, ac(workspace_domain.ResourceWhatsAppCampaigns, read, h.GetSection))
	protected.HandleFunc("/attendance/campaigns/{section}", section).Methods(http.MethodGet)
}
