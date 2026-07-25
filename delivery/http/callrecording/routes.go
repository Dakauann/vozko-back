package callrecording

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterAdminRoutes(admin *mux.Router, h *CallRecordingHandler) {
	admin.HandleFunc("/admin/call-recordings", h.ListRecordings).Methods(http.MethodGet)
	admin.HandleFunc("/admin/call-recordings/by-call", h.GetByCallID).Methods(http.MethodGet)
	admin.HandleFunc("/admin/call-recordings/by-lead", h.GetByLeadID).Methods(http.MethodGet)
}

func RegisterRoutes(
	protected *mux.Router,
	h *CallRecordingHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	cr := workspace_domain.ResourceCallRecordings
	protected.HandleFunc("/call-recordings/by-entry", ac(cr, workspace_domain.ActionRead, h.GetByEntryID)).Methods(http.MethodGet)
}
