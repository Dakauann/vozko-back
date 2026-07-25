package workspacetemplateaccess

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterAdminRoutes(admin *mux.Router, h *WorkspaceTemplateAccessHandler) {
	admin.HandleFunc("/admin/template-access", h.GrantAccess).Methods(http.MethodPost)
	admin.HandleFunc("/admin/template-access", h.RevokeAccess).Methods(http.MethodDelete)
	admin.HandleFunc("/admin/workspaces/{workspaceId}/template-access", h.ListByWorkspace).Methods(http.MethodGet)
	admin.HandleFunc("/admin/templates/{templateId}/access", h.ListByTemplate).Methods(http.MethodGet)
}
