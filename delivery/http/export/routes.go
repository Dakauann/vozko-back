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
	// Workspace-wide first: it is the more specific literal path, and reading it
	// requires exactly what reading the disparos summary it mirrors requires.
	protected.HandleFunc("/whatsapp/campaigns/entries/export", ac(wc, workspace_domain.ActionRead, h.ExportWhatsAppWorkspaceEntries)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp/campaigns/{id}/entries/export", ac(wc, workspace_domain.ActionRead, h.ExportWhatsAppEntries)).Methods(http.MethodGet)

	// Channels with no campaign export per account, or workspace-wide when no
	// account is named. Gated on each channel's own resource so export never
	// grants more than the channel itself does.
	protected.HandleFunc("/instagram/accounts/{id}/entries/export",
		ac(workspace_domain.ResourceInstagramAccounts, workspace_domain.ActionRead, h.ExportInstagramEntries)).Methods(http.MethodGet)
	protected.HandleFunc("/telegram/accounts/{id}/entries/export",
		ac(workspace_domain.ResourceTelegramAccounts, workspace_domain.ActionRead, h.ExportTelegramEntries)).Methods(http.MethodGet)
}
