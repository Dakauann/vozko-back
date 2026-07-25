package opportunityboard

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *OpportunityBoardHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	op := workspace_domain.ResourceConversations
	protected.HandleFunc("/opportunities/board", ac(op, workspace_domain.ActionRead, h.Board)).Methods(http.MethodGet)
	protected.HandleFunc("/opportunities/list", ac(op, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
}
