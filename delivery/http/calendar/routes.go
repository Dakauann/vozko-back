package calendar

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterPublicRoutes(public *mux.Router, h *CalendarHandler) {
	public.HandleFunc("/calendar/google/callback", h.GoogleOAuthCallback).Methods(http.MethodGet)
	public.HandleFunc("/webhooks/google-calendar", h.HandleGoogleCalendarWebhook).Methods(http.MethodPost)
}

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *CalendarHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	cal := workspace_domain.ResourceCalendar
	protected.HandleFunc("/calendar/events", ac(cal, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/calendar/events", ac(cal, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	protected.HandleFunc("/calendar/events/{id}", ac(cal, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	protected.HandleFunc("/calendar/events/{id}", ac(cal, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPut)
	protected.HandleFunc("/calendar/events/{id}", ac(cal, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
	protected.HandleFunc("/calendar/google/connect", ac(cal, workspace_domain.ActionCreate, h.ConnectGoogle)).Methods(http.MethodPost)
	protected.HandleFunc("/calendar/google/disconnect", ac(cal, workspace_domain.ActionDelete, h.DisconnectGoogle)).Methods(http.MethodDelete)
	protected.HandleFunc("/calendar/google/status", ac(cal, workspace_domain.ActionRead, h.GetConnection)).Methods(http.MethodGet)
	protected.HandleFunc("/calendar/google/auth-url", ac(cal, workspace_domain.ActionRead, h.GetAuthURL)).Methods(http.MethodGet)
	protected.HandleFunc("/calendar/google/watch", ac(cal, workspace_domain.ActionCreate, h.StartWatch)).Methods(http.MethodPost)
	protected.HandleFunc("/calendar/google/watch", ac(cal, workspace_domain.ActionDelete, h.StopWatch)).Methods(http.MethodDelete)
}
