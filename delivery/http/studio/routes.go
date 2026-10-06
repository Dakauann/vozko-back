package studiohttp

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
	protected.HandleFunc("/studio/projects", ac(media, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/studio/projects", ac(media, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	protected.HandleFunc("/studio/projects/{id}", ac(media, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	protected.HandleFunc("/studio/projects/{id}", ac(media, workspace_domain.ActionCreate, h.Save)).Methods(http.MethodPatch)
	protected.HandleFunc("/studio/projects/{id}", ac(media, workspace_domain.ActionDelete, h.Archive)).Methods(http.MethodDelete)
	protected.HandleFunc("/studio/projects/{id}/export", ac(media, workspace_domain.ActionCreate, h.Export)).Methods(http.MethodPost)
}
