package http

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func (r *router) setupPublicMCPRoutes() {
	if r.agentMCP == nil || r.agentMCP.OAuthCallback == nil {
		return
	}
	r.mux.HandleFunc(
		"/mcp/oauth/callback",
		r.agentMCP.OAuthCallback.Callback,
	).Methods(http.MethodGet)
}

func (r *router) setupMCPRoutes(protected *mux.Router) {
	if r.agentMCP == nil {
		return
	}
	res := workspace_domain.ResourceMCP

	if h := r.agentMCP.Builtin; h != nil {
		protected.HandleFunc("/mcp/catalog", r.ac(res, workspace_domain.ActionRead, h.ListCatalog)).Methods(http.MethodGet)
		protected.HandleFunc("/mcp/bindings", r.ac(res, workspace_domain.ActionRead, h.ListBindings)).Methods(http.MethodGet)
		protected.HandleFunc("/mcp/bindings", r.ac(res, workspace_domain.ActionCreate, h.Enable)).Methods(http.MethodPost)
		protected.HandleFunc("/mcp/bindings/{id}/api-key", r.ac(res, workspace_domain.ActionUpdate, h.ConfigureAPIKey)).Methods(http.MethodPut)
		protected.HandleFunc("/mcp/bindings/{id}/oauth/start", r.ac(res, workspace_domain.ActionUpdate, h.StartOAuth2)).Methods(http.MethodPost)
		protected.HandleFunc("/mcp/bindings/{id}", r.ac(res, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
	}

	if h := r.agentMCP.Remote; h != nil {
		protected.HandleFunc("/mcp/remote", r.ac(res, workspace_domain.ActionCreate, h.Register)).Methods(http.MethodPost)
		protected.HandleFunc("/mcp/remote", r.ac(res, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
		protected.HandleFunc("/mcp/remote/{id}", r.ac(res, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
		protected.HandleFunc("/mcp/remote/{id}", r.ac(res, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
		protected.HandleFunc("/mcp/remote/{id}/oauth/start", r.ac(res, workspace_domain.ActionUpdate, h.StartOAuthFlow)).Methods(http.MethodPost)
	}

	if h := r.agentMCP.Collection; h != nil {
		protected.HandleFunc("/mcp/collections", r.ac(res, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
		protected.HandleFunc("/mcp/collections", r.ac(res, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
		protected.HandleFunc("/mcp/collections/{id}", r.ac(res, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
		protected.HandleFunc("/mcp/collections/{id}", r.ac(res, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPut)
		protected.HandleFunc("/mcp/collections/{id}", r.ac(res, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
	}
}
