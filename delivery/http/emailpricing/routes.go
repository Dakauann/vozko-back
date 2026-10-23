package emailpricing

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	protected.HandleFunc("/email/pricing", ac(workspace_domain.ResourceEmailMarketing, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
}
