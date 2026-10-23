package apikeys

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
	res := workspace_domain.ResourceAPIKeys
	protected.HandleFunc("/api-keys", ac(res, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/api-keys", ac(res, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	protected.HandleFunc("/api-keys/{id}", ac(res, workspace_domain.ActionDelete, h.Revoke)).Methods(http.MethodDelete)
}
