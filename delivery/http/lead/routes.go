package lead

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *LeadHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	ld := workspace_domain.ResourceLeads
	ldRoutes := protected.PathPrefix("/leads").Subrouter()
	ldRoutes.HandleFunc("", ac(ld, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	ldRoutes.HandleFunc("/search", ac(ld, workspace_domain.ActionRead, h.GetByNumber)).Methods(http.MethodGet)
	ldRoutes.HandleFunc("/{id}", ac(ld, workspace_domain.ActionRead, h.GetByID)).Methods(http.MethodGet)
	ldRoutes.HandleFunc("/{id}/block", ac(ld, workspace_domain.ActionBlock, h.BlockLead)).Methods(http.MethodPost)
	ldRoutes.HandleFunc("/{id}/campaigns", ac(ld, workspace_domain.ActionRead, h.GetCampaignHistory)).Methods(http.MethodGet)
	ldRoutes.HandleFunc("/{id}/conversations", ac(ld, workspace_domain.ActionRead, h.GetConversationHistory)).Methods(http.MethodGet)
	ldRoutes.HandleFunc("/{id}/campaigns/{campaignId}/entries", ac(ld, workspace_domain.ActionRead, h.GetEntriesByCampaign)).Methods(http.MethodGet)
	ldRoutes.HandleFunc("/{id}/campaigns/{campaignId}/analysis", ac(ld, workspace_domain.ActionRead, h.GetAnalysisByCampaign)).Methods(http.MethodGet)
}

func RegisterEntryConversationRoutes(
	protected *mux.Router,
	h *LeadHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	cv := workspace_domain.ResourceConversations
	protected.HandleFunc("/entries/{entryId}/conversation", ac(cv, workspace_domain.ActionRead, h.GetConversationByEntry)).Methods(http.MethodGet)
}
