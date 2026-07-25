package workspaceconfig

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterProtectedRoutes(protected *mux.Router, h *WorkspaceConfigHandler) {
	protected.HandleFunc("/workspaces/{workspaceId}/config", h.Get).Methods(http.MethodGet)
	protected.HandleFunc("/workspaces/{workspaceId}/config", h.Update).Methods(http.MethodPut)
}

func RegisterAdminRoutes(admin *mux.Router, h *WorkspaceConfigHandler) {
	admin.HandleFunc("/admin/workspaces/{workspaceId}/config", h.Get).Methods(http.MethodGet)
	admin.HandleFunc("/admin/workspaces/{workspaceId}/config", h.UpdateSensitive).Methods(http.MethodPut)
}
