package crmbulk

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *CRMBulkHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	cv := workspace_domain.ResourceConversations
	protected.HandleFunc("/crm/bulk", ac(cv, workspace_domain.ActionUpdate, h.Bulk)).Methods(http.MethodPost)
}
