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
	protected.HandleFunc("/whatsapp/campaigns/entries/export", ac(wc, workspace_domain.ActionRead, h.ExportWhatsAppWorkspaceEntries)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp/campaigns/{id}/entries/export", ac(wc, workspace_domain.ActionRead, h.ExportWhatsAppEntries)).Methods(http.MethodGet)

	protected.HandleFunc("/instagram/accounts/{id}/entries/export",
		ac(workspace_domain.ResourceInstagramAccounts, workspace_domain.ActionRead, h.ExportInstagramEntries)).Methods(http.MethodGet)
	protected.HandleFunc("/telegram/accounts/{id}/entries/export",
		ac(workspace_domain.ResourceTelegramAccounts, workspace_domain.ActionRead, h.ExportTelegramEntries)).Methods(http.MethodGet)

	protected.HandleFunc("/unofficial-whatsapp/campaigns/{id}/entries/export",
		ac(workspace_domain.ResourceUnofficialWhatsAppCampaigns, workspace_domain.ActionRead,
			h.ExportUnofficialWhatsAppCampaignEntries)).Methods(http.MethodGet)
}
