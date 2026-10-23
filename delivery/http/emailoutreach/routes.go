package emailoutreach

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
	if h == nil {
		return
	}
	res := workspace_domain.ResourceEmailMarketing
	protected.HandleFunc("/email/outreach/conversations", ac(res, workspace_domain.ActionStart, h.StartConversation)).Methods(http.MethodPost)
	protected.HandleFunc("/email/outreach/quote", ac(res, workspace_domain.ActionRead, h.Quote)).Methods(http.MethodGet)
}
