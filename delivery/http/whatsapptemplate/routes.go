package whatsapptemplate

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *WhatsAppTemplateHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	wt := workspace_domain.ResourceWhatsAppTemplates
	protected.HandleFunc("/whatsapp/templates", ac(wt, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp/templates/{id}", ac(wt, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)

	protected.HandleFunc("/whatsapp/templates", ac(wt, workspace_domain.ActionCreate, h.CreateForWorkspace)).Methods(http.MethodPost)
	protected.HandleFunc("/whatsapp/templates/{id}", ac(wt, workspace_domain.ActionDelete, h.DeleteForWorkspace)).Methods(http.MethodDelete)
	protected.HandleFunc("/whatsapp/templates/sync", ac(wt, workspace_domain.ActionUpdate, h.SyncForWorkspace)).Methods(http.MethodPost)
	protected.HandleFunc("/whatsapp/templates/{id}/sync", ac(wt, workspace_domain.ActionUpdate, h.SyncOneForWorkspace)).Methods(http.MethodPost)
	protected.HandleFunc("/whatsapp/templates/{id}/header-media", ac(wt, workspace_domain.ActionUpdate, h.UpdateHeaderMediaForWorkspace)).Methods(http.MethodPatch)
}

func RegisterAdminRoutes(admin *mux.Router, h *WhatsAppTemplateHandler) {
	admin.HandleFunc("/admin/whatsapp/templates", h.ListAll).Methods(http.MethodGet)
	admin.HandleFunc("/admin/whatsapp/templates/{id}", h.GetAdmin).Methods(http.MethodGet)
	admin.HandleFunc("/whatsapp/templates", h.Create).Methods(http.MethodPost)
	admin.HandleFunc("/whatsapp/templates/sync", h.Sync).Methods(http.MethodPost)
	admin.HandleFunc("/whatsapp/templates/send", h.Send).Methods(http.MethodPost)
	admin.HandleFunc("/whatsapp/templates/{id}/sync", h.SyncOne).Methods(http.MethodPost)
	admin.HandleFunc("/whatsapp/templates/{id}/replicate", h.Replicate).Methods(http.MethodPost)
	admin.HandleFunc("/whatsapp/templates/{id}/header-media", h.UpdateHeaderMediaURL).Methods(http.MethodPatch)
}
