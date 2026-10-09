package calllisthttp

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	workspace_domain "vozko/domain/workspace"
)

const uuidPattern = httpx.UUIDPattern

func RegisterProtectedRoutes(protected *mux.Router, h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc) {
	if h == nil {
		return
	}
	res := workspace_domain.ResourceCallLists
	read, manage := workspace_domain.ActionRead, workspace_domain.ActionManage
	list := "/call-lists/{listId:" + uuidPattern + "}"
	item := "/call-list-items/{itemId:" + uuidPattern + "}"
	protected.HandleFunc("/call-lists", ac(res, read, h.List)).Methods(http.MethodGet)
	protected.HandleFunc(list, ac(res, read, h.Get)).Methods(http.MethodGet)
	protected.HandleFunc(list, ac(res, manage, h.Update)).Methods(http.MethodPatch)
	protected.HandleFunc(list, ac(res, manage, h.Delete)).Methods(http.MethodDelete)
	protected.HandleFunc(list+"/items", ac(res, read, h.Items)).Methods(http.MethodGet)
	protected.HandleFunc(list+"/next", ac(res, read, h.Next)).Methods(http.MethodPost)
	protected.HandleFunc(item+"/release", ac(res, read, h.Release)).Methods(http.MethodPost)
	protected.HandleFunc(item+"/close", ac(res, read, h.Close)).Methods(http.MethodPost)
}
