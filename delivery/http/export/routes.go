package export

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *ExportHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	wc := workspace_domain.ResourceWhatsAppCampaigns
	protected.HandleFunc("/whatsapp/campaigns/{id}/entries/export", ac(wc, workspace_domain.ActionRead, h.ExportWhatsAppEntries)).Methods(http.MethodGet)
}
