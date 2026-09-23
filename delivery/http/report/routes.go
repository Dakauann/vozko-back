package report

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterPublicRoutes(mux *mux.Router, h *ReportHandler) {
	mux.HandleFunc("/reports/{id}/print-data", h.PrintData).Methods(http.MethodGet)
}

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *ReportHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	res := workspace_domain.ResourceReports
	routes := protected.PathPrefix("/reports").Subrouter()

	routes.HandleFunc("", ac(res, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	routes.HandleFunc("", ac(res, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	routes.HandleFunc("/kinds", ac(res, workspace_domain.ActionRead, h.Kinds)).Methods(http.MethodGet)
	routes.HandleFunc("/{id}", ac(res, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	routes.HandleFunc("/{id}/file", ac(res, workspace_domain.ActionRead, h.Download)).Methods(http.MethodGet)
}
