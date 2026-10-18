package customerportfoliohttp

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	workspace_domain "vozko/domain/workspace"
)

const (
	entriesPath      = "/customer-portfolio/entries"
	entryPath        = entriesPath + "/{entryId:" + httpx.UUIDPattern + "}"
	leadPath         = "/customer-portfolio/leads/{leadId:" + httpx.UUIDPattern + "}"
	conversationPath = "/customer-portfolio/conversations/{entryType}/{entryId:" + httpx.UUIDPattern + "}"
)

func RegisterRoutes(protected *mux.Router, h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc) {
	if h == nil {
		h = NewHandler(nil)
	}
	res := workspace_domain.ResourceCustomerPortfolios
	read, use, manage := workspace_domain.ActionRead, workspace_domain.ActionUse, workspace_domain.ActionManage
	protected.HandleFunc(entriesPath, ac(res, read, h.List)).Methods(http.MethodGet)
	protected.HandleFunc(entryPath, ac(res, manage, h.Move)).Methods(http.MethodPut)
	protected.HandleFunc(entryPath, ac(res, use, h.Remove)).Methods(http.MethodDelete)
	protected.HandleFunc(leadPath, ac(res, read, h.ForLead)).Methods(http.MethodGet)
	protected.HandleFunc(leadPath, ac(res, use, h.PlaceLead)).Methods(http.MethodPost)
	protected.HandleFunc(conversationPath, ac(res, read, h.ForConversation)).Methods(http.MethodGet)
	protected.HandleFunc(conversationPath, ac(res, use, h.PlaceConversation)).Methods(http.MethodPost)
}
