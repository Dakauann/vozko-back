package emailhygienehttp

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		return
	}
	protected.HandleFunc("/email/address-status", ac(workspace_domain.ResourceEmailMarketing, workspace_domain.ActionRead, h.AddressStatus)).Methods(http.MethodGet)
}
