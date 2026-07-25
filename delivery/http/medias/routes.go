package medias

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

type RateLimiter interface {
	Validate(next http.Handler) http.Handler
}

func RegisterRoutes(
	mediasRoutes *mux.Router,
	h *MediasHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
	uploadRateLimiter RateLimiter,
) {
	md := workspace_domain.ResourceMedia

	uploadRoute := mediasRoutes.PathPrefix("/medias").Subrouter()
	uploadRoute.Use(uploadRateLimiter.Validate)
	uploadRoute.HandleFunc("", ac(md, workspace_domain.ActionCreate, h.UploadMedia)).Methods(http.MethodPost)

	mediasRoutes.HandleFunc("/medias", ac(md, workspace_domain.ActionRead, h.ListMedias)).Methods(http.MethodGet)
	mediasRoutes.HandleFunc("/medias/{id}", ac(md, workspace_domain.ActionRead, h.GetMedia)).Methods(http.MethodGet)
	mediasRoutes.HandleFunc("/medias/{id}", ac(md, workspace_domain.ActionDelete, h.DeleteHoldMusic)).Methods(http.MethodDelete)
}
