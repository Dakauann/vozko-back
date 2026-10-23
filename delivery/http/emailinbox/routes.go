package emailinbox

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
	res :=workspace_domain.ResourceEmailInboxes
	inboxes := protected.PathPrefix("/email/inboxes").Subrouter()

	inboxes.HandleFunc("", ac(res, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	inboxes.HandleFunc("/{senderId}", ac(res, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	inboxes.HandleFunc("/{senderId}", ac(res, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPut)
}
