package callbilling

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *CallBillingHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	cb := workspace_domain.ResourceCallRecordings
	protected.HandleFunc("/call-billing", ac(cb, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
}
