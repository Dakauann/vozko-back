package commentanalysis

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

// RegisterProtectedRoutes wires the authenticated comment-analysis routes
// (plan §12). Reads are ActionRead; moderation, configuration, retries and
// backfills are ActionUpdate on the comment_analysis resource.
func RegisterProtectedRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	// A nil handler means the feature is not wired; register nothing rather
	// than routes whose methods would nil-panic on the first request.
	if h == nil {
		return
	}
	res := workspace_domain.ResourceCommentAnalysis
	read, update := workspace_domain.ActionRead, workspace_domain.ActionUpdate

	r := protected.PathPrefix("/comment-analysis").Subrouter()

	r.HandleFunc("", ac(res, read, h.List)).Methods(http.MethodGet)
	r.HandleFunc("/stats", ac(res, read, h.Stats)).Methods(http.MethodGet)
	r.HandleFunc("/trends", ac(res, read, h.Trends)).Methods(http.MethodGet)
	r.HandleFunc("/spend", ac(res, read, h.Spend)).Methods(http.MethodGet)

	r.HandleFunc("/authors", ac(res, read, h.ListAuthors)).Methods(http.MethodGet)
	r.HandleFunc("/authors/{id}", ac(res, read, h.GetAuthor)).Methods(http.MethodGet)
	r.HandleFunc("/authors/{id}", ac(res, update, h.SetModeration)).Methods(http.MethodPatch)

	r.HandleFunc("/settings", ac(res, read, h.ListAccountSettings)).Methods(http.MethodGet)
	r.HandleFunc("/settings/{source}/{accountId}", ac(res, read, h.GetSettings)).Methods(http.MethodGet)
	r.HandleFunc("/settings/{source}/{accountId}", ac(res, update, h.UpdateSettings)).Methods(http.MethodPatch)
	// A post's own settings, layered over the account's.
	r.HandleFunc("/settings/{source}/{accountId}/containers/{containerId}", ac(res, read, h.GetContainerSettings)).Methods(http.MethodGet)
	r.HandleFunc("/settings/{source}/{accountId}/containers/{containerId}", ac(res, update, h.PutContainerSettings)).Methods(http.MethodPut)
	r.HandleFunc("/settings/{source}/{accountId}/containers/{containerId}", ac(res, update, h.DeleteContainerSettings)).Methods(http.MethodDelete)

	// Backfill: the two account-scoped routes before the id-scoped ones, so
	// "{source}/{accountId}/estimate" never matches "{id}".
	r.HandleFunc("/backfill/{source}/{accountId}/estimate", ac(res, read, h.EstimateBackfill)).Methods(http.MethodGet)
	r.HandleFunc("/backfill/{source}/{accountId}", ac(res, update, h.StartBackfill)).Methods(http.MethodPost)
	r.HandleFunc("/backfill/{id}/cancel", ac(res, update, h.CancelBackfill)).Methods(http.MethodPost)
	r.HandleFunc("/backfill/{id}", ac(res, read, h.GetBackfill)).Methods(http.MethodGet)

	r.HandleFunc("/{id}/retry", ac(res, update, h.Retry)).Methods(http.MethodPost)
}
