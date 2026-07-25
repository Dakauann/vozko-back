package waba

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *WABAHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	bp := workspace_domain.ResourceBusinessPhones
	protected.HandleFunc("/whatsapp/wabas", ac(bp, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp/wabas/{id}", ac(bp, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
}
