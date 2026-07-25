package holdmusic

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *HoldMusicHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	md := workspace_domain.ResourceMedia
	protected.HandleFunc("/hold-music/builtins", ac(md, workspace_domain.ActionRead, h.ListBuiltins)).Methods(http.MethodGet)
	protected.HandleFunc("/hold-music/builtins/{key}/audio", ac(md, workspace_domain.ActionRead, h.StreamBuiltin)).Methods(http.MethodGet)
}
