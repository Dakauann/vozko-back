package invoice

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterUserRoutes(
	protected *mux.Router,
	h *InvoiceHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	pl := workspace_domain.ResourcePlans
	invoiceRoutes := protected.PathPrefix("/user/invoices").Subrouter()
	invoiceRoutes.HandleFunc("", ac(pl, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	invoiceRoutes.HandleFunc("", ac(pl, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	invoiceRoutes.HandleFunc("/{id}", ac(pl, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
}

func RegisterAdminRoutes(adminRoutes *mux.Router, h *InvoiceHandler) {
	adminRoutes.HandleFunc("/admin/balances/{workspaceId}/invoices", h.AdminCreate).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/admin/balances/{workspaceId}/invoices", h.AdminList).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/balances/{workspaceId}/invoices/{id}", h.AdminGet).Methods(http.MethodGet)
}
