package workspacephoneaccess

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterAdminRoutes(admin *mux.Router, h *WorkspacePhoneAccessHandler) {
	admin.HandleFunc("/admin/phone-access", h.GrantAccess).Methods(http.MethodPost)
	admin.HandleFunc("/admin/phone-access", h.RevokeAccess).Methods(http.MethodDelete)
	admin.HandleFunc("/admin/workspaces/{workspaceId}/phone-access", h.ListByWorkspace).Methods(http.MethodGet)
	admin.HandleFunc("/admin/phones/{phoneId}/access", h.ListByPhone).Methods(http.MethodGet)
}
