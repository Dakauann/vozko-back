package shortlink

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

type RateLimiter interface {
	Validate(next http.Handler) http.Handler
}

func RegisterRoutes(
	protected *mux.Router,
	h *ShortLinkHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	res := workspace_domain.ResourceShortLinks
	sl := protected.PathPrefix("/short-links").Subrouter()
	sl.HandleFunc("", ac(res, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	sl.HandleFunc("", ac(res, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	sl.HandleFunc("/stats", ac(res, workspace_domain.ActionRead, h.Stats)).Methods(http.MethodGet)
	sl.HandleFunc("/{id}", ac(res, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	sl.HandleFunc("/{id}", ac(res, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPatch)
	sl.HandleFunc("/{id}", ac(res, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
	sl.HandleFunc("/{id}/analytics", ac(res, workspace_domain.ActionRead, h.Analytics)).Methods(http.MethodGet)
	sl.HandleFunc("/{id}/clicks", ac(res, workspace_domain.ActionRead, h.RecentClicks)).Methods(http.MethodGet)
	sl.HandleFunc("/{id}/qr", ac(res, workspace_domain.ActionRead, h.QR)).Methods(http.MethodGet)
}

func RegisterPublicRoutes(public *mux.Router, h *ShortLinkHandler, rl RateLimiter) {
	public.Handle("/r/{code}", rl.Validate(http.HandlerFunc(h.Redirect))).Methods(http.MethodGet)
	public.Handle("/r/{code}/unlock", rl.Validate(http.HandlerFunc(h.Unlock))).Methods(http.MethodPost)
}
