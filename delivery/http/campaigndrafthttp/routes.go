package campaigndrafthttp

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

type AccessControl func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc

func RegisterProtectedRoutes(protected *mux.Router, h *Handler, ac AccessControl, resource workspace_domain.Resource) {
	if h == nil || ac == nil {
		return
	}
	h.start = StartGuard(ac, resource)
	base := protected.PathPrefix("/campaign-drafts/" + string(h.channel)).Subrouter()
	base.HandleFunc("", ac(resource, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	base.HandleFunc("", ac(resource, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	base.HandleFunc("/{id}", ac(resource, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	base.HandleFunc("/{id}", ac(resource, workspace_domain.ActionCreate, h.Update)).Methods(http.MethodPut)
	base.HandleFunc("/{id}", ac(resource, workspace_domain.ActionCreate, h.Delete)).Methods(http.MethodDelete)
	base.HandleFunc("/{id}/review", ac(resource, workspace_domain.ActionCreate, h.Review)).Methods(http.MethodPost)
	base.HandleFunc("/{id}/launch", ac(resource, workspace_domain.ActionCreate, h.Launch)).Methods(http.MethodPost)
}

func StartGuard(ac AccessControl, resource workspace_domain.Resource) Guard {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return ac(resource, workspace_domain.ActionStart, next)
	}
}
