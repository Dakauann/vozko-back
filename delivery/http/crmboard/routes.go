package crmboard

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *CRMBoardHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	cv := workspace_domain.ResourceConversations
	protected.HandleFunc("/crm/board", ac(cv, workspace_domain.ActionRead, h.Board)).Methods(http.MethodGet)
	protected.HandleFunc("/crm/entries", ac(cv, workspace_domain.ActionRead, h.Entries)).Methods(http.MethodGet)
}
