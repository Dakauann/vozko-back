package imagegenhttp

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

type AccessControl func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc

func RegisterProtectedRoutes(protected *mux.Router, h *Handler, ac AccessControl) {
	if h == nil {
		return
	}
	media := workspace_domain.ResourceMedia
	protected.HandleFunc("/images/generations", ac(media, workspace_domain.ActionCreate, h.Generate)).Methods(http.MethodPost)
	protected.HandleFunc("/images/generations/{id}", ac(media, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
}
