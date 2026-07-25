package supportinbox

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *SupportInboxHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	si := workspace_domain.ResourceSupportInboxes
	siRoutes := protected.PathPrefix("/support/inboxes").Subrouter()
	siRoutes.HandleFunc("", ac(si, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	siRoutes.HandleFunc("", ac(si, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	siRoutes.HandleFunc("/{id}", ac(si, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	siRoutes.HandleFunc("/{id}", ac(si, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPut)
	siRoutes.HandleFunc("/{id}", ac(si, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
}

func RegisterPublicRoutes(public *mux.Router, h *SupportInboxHandler) {
	public.HandleFunc("/public/support/sessions", h.CreateSession).Methods(http.MethodPost)
	public.HandleFunc("/public/support/sessions", h.CreateSessionPreflight).Methods(http.MethodOptions)
	public.HandleFunc("/public/support/sessions/reconnect", h.ReconnectSession).Methods(http.MethodPost)
	public.HandleFunc("/public/support/sessions/reconnect", h.CreateSessionPreflight).Methods(http.MethodOptions)
	public.HandleFunc("/public/support/widget/{inboxId}", h.GetWidgetInfo).Methods(http.MethodGet)
	public.HandleFunc("/public/support/widget/{inboxId}", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
	}).Methods(http.MethodOptions)
}
