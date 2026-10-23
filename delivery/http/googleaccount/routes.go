package googleaccount

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

const CallbackPath = "/oauth/google/callback"

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	cal, sheets := workspace_domain.ResourceCalendar, workspace_domain.ResourceGoogleSheets
	protected.HandleFunc("/oauth/google/calendar/start", ac(cal, workspace_domain.ActionCreate, h.StartCalendar)).Methods(http.MethodGet)
	protected.HandleFunc("/oauth/google/sheets/start", ac(sheets, workspace_domain.ActionCreate, h.StartSheets)).Methods(http.MethodGet)
	protected.HandleFunc("/google/accounts/calendar", ac(cal, workspace_domain.ActionRead, h.CalendarStatus)).Methods(http.MethodGet)
	protected.HandleFunc("/google/accounts/sheets", ac(sheets, workspace_domain.ActionRead, h.SheetsStatus)).Methods(http.MethodGet)
	protected.HandleFunc("/google/accounts/calendar", ac(cal, workspace_domain.ActionDelete, h.DisconnectCalendar)).Methods(http.MethodDelete)
	protected.HandleFunc("/google/accounts/sheets", ac(sheets, workspace_domain.ActionDelete, h.DisconnectSheets)).Methods(http.MethodDelete)
}

func RegisterPublicRoutes(public *mux.Router, h *Handler) {
	public.HandleFunc(CallbackPath, h.Callback).Methods(http.MethodGet)
}
