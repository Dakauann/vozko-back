package googlesheet

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
	res := workspace_domain.ResourceGoogleSheets
	protected.HandleFunc("/google-sheets/catalog", ac(res, workspace_domain.ActionRead, h.Catalog)).Methods(http.MethodGet)
	protected.HandleFunc("/google-sheets", ac(res, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/google-sheets", ac(res, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	protected.HandleFunc("/google-sheets/{id}", ac(res, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	protected.HandleFunc("/google-sheets/{id}", ac(res, workspace_domain.ActionUpdate, h.UpdateDetails)).Methods(http.MethodPatch)
	protected.HandleFunc("/google-sheets/{id}", ac(res, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
	protected.HandleFunc("/google-sheets/{id}/columns", ac(res, workspace_domain.ActionUpdate, h.EditColumns)).Methods(http.MethodPatch)
	protected.HandleFunc("/google-sheets/{id}/health", ac(res, workspace_domain.ActionRead, h.Health)).Methods(http.MethodGet)
	protected.HandleFunc("/google-sheets/{id}/retry", ac(res, workspace_domain.ActionUpdate, h.Retry)).Methods(http.MethodPost)
	protected.HandleFunc("/google-sheets/{id}/reactivate", ac(res, workspace_domain.ActionUpdate, h.Reactivate)).Methods(http.MethodPost)
	protected.HandleFunc("/google-sheets/{id}/rows", ac(res, workspace_domain.ActionUpdate, h.AddRow)).Methods(http.MethodPost)
	protected.HandleFunc("/google-sheets/{id}/rows", ac(res, workspace_domain.ActionRead, h.ListRows)).Methods(http.MethodGet)
}
