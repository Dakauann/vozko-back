package workspacepricing

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterRoutes(protected *mux.Router, h *WorkspacePricingHandler) {
	protected.HandleFunc("/pricing/exchange-rate", h.GetPublicExchangeRate).Methods(http.MethodGet)
}

func RegisterAdminRoutes(adminRoutes *mux.Router, h *WorkspacePricingHandler) {
	adminRoutes.HandleFunc("/admin/pricing/defaults", h.GetDefaults).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/pricing/defaults", h.UpdateDefaultItem).Methods(http.MethodPut)
	adminRoutes.HandleFunc("/admin/pricing/audit", h.GetAuditLog).Methods(http.MethodGet)

	adminRoutes.HandleFunc("/admin/workspaces/{workspaceId}/pricing/resolved", h.GetResolved).Methods(http.MethodGet)

	adminRoutes.HandleFunc("/admin/pricing/exchange-rate", h.GetExchangeRate).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/pricing/exchange-rate", h.UpdateExchangeRate).Methods(http.MethodPut)

}
