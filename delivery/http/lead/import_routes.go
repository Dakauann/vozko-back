package lead

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

type RateLimiter interface {
	Validate(next http.Handler) http.Handler
}

func RegisterImportRoutes(
	protected *mux.Router,
	h *LeadHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
	uploadLimiter RateLimiter,
) {
	ld := workspace_domain.ResourceLeads
	uploads := protected.PathPrefix("/leads").Subrouter()
	uploads.Use(uploadLimiter.Validate)
	uploads.HandleFunc("/imports", ac(ld, workspace_domain.ActionCreate, h.CreateImport)).Methods(http.MethodPost)

	protected.HandleFunc("/leads/imports", ac(ld, workspace_domain.ActionCreate, h.ListImports)).Methods(http.MethodGet)

	imports := protected.PathPrefix("/leads/imports").Subrouter()
	imports.HandleFunc(leadIDPath, ac(ld, workspace_domain.ActionCreate, h.GetImport)).Methods(http.MethodGet)
	imports.HandleFunc(leadIDPath+"/dry-run", ac(ld, workspace_domain.ActionCreate, h.DryRunImport)).Methods(http.MethodPost)
	imports.HandleFunc(leadIDPath+"/start", ac(ld, workspace_domain.ActionCreate, h.StartImport)).Methods(http.MethodPost)
	imports.HandleFunc(leadIDPath+"/rejections", ac(ld, workspace_domain.ActionCreate, h.ImportRejections)).Methods(http.MethodGet)
}
