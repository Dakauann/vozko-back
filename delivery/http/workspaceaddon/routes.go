package workspaceaddon

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *WorkspaceAddonHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	pl := workspace_domain.ResourcePlans
	protected.HandleFunc("/workspaces/{workspaceId}/addons/available", h.ListAvailable).Methods(http.MethodGet)
	protected.HandleFunc("/workspaces/{workspaceId}/addons/preview", h.Preview).Methods(http.MethodPost)
	protected.HandleFunc("/workspaces/{workspaceId}/addons", h.ListWorkspace).Methods(http.MethodGet)
	protected.HandleFunc("/workspaces/{workspaceId}/addons", ac(pl, workspace_domain.ActionCreate, h.Purchase)).Methods(http.MethodPost)
	protected.HandleFunc("/workspaces/{workspaceId}/addons/{addonSubscriptionId}/cancel", ac(pl, workspace_domain.ActionDelete, h.Cancel)).Methods(http.MethodPost)
	protected.HandleFunc("/workspaces/{workspaceId}/entitlements", h.GetEntitlements).Methods(http.MethodGet)
}

func RegisterAdminRoutes(adminRoutes *mux.Router, h *WorkspaceAddonHandler) {
	adminRoutes.HandleFunc("/admin/addons", h.ListAdmin).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/addons", h.Create).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/admin/addons/{addonId}", h.Get).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/addons/{addonId}", h.Update).Methods(http.MethodPut)
	adminRoutes.HandleFunc("/admin/addons/{addonId}/archive", h.Archive).Methods(http.MethodPatch)
}
