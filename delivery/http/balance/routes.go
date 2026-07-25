package balance

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *BalanceHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	bl := workspace_domain.ResourceBalance
	balanceRoutes := protected.PathPrefix("/user/balance").Subrouter()
	balanceRoutes.HandleFunc("", ac(bl, workspace_domain.ActionRead, h.GetMyBalance)).Methods(http.MethodGet)
	balanceRoutes.HandleFunc("/transactions", ac(bl, workspace_domain.ActionRead, h.ListMyTransactions)).Methods(http.MethodGet)
	balanceRoutes.HandleFunc("/transactions/export", ac(bl, workspace_domain.ActionRead, h.ExportMyTransactions)).Methods(http.MethodGet)
	balanceRoutes.HandleFunc("/service-types", ac(bl, workspace_domain.ActionRead, h.ListServiceTypes)).Methods(http.MethodGet)
}

func RegisterAdminRoutes(adminRoutes *mux.Router, h *BalanceHandler) {
	adminRoutes.HandleFunc("/admin/balances", h.Create).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/admin/balances/{workspaceId}", h.GetByWorkspaceID).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/balances/{workspaceId}/summary", h.GetFullSummaryByWorkspaceID).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/balances/{workspaceId}/debit", h.Debit).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/admin/balances/{workspaceId}/debit-resource", h.DebitResource).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/admin/balances/{workspaceId}/transactions", h.ListTransactions).Methods(http.MethodGet)
}
