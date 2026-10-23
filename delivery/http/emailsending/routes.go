package emailsending

import (
	"net/http"

	"github.com/gorilla/mux"
)

type Guard func(http.HandlerFunc) http.HandlerFunc

type Guards struct {
	View   Guard
	Manage Guard
}

func RegisterProtectedRoutes(protected *mux.Router, h *Handler, g Guards) {
	if protected == nil || g.View == nil || g.Manage == nil {
		panic("email http: routes need the router and both access guards")
	}
	email := protected.PathPrefix("/email").Subrouter()
	email.HandleFunc("/setup", g.View(h.GetSetup)).Methods(http.MethodGet)
	email.HandleFunc("/setup/provision", g.Manage(h.Provision)).Methods(http.MethodPost)
	email.HandleFunc("/setup/subdomain/check", g.Manage(h.CheckSubdomain)).Methods(http.MethodGet)
	email.HandleFunc("/setup/subdomain", g.Manage(h.ChangeSubdomain)).Methods(http.MethodPut)
	email.HandleFunc("/domains", g.Manage(h.ConnectDomain)).Methods(http.MethodPost)
	email.HandleFunc("/domains/{id}", g.View(h.GetDomain)).Methods(http.MethodGet)
	email.HandleFunc("/domains/{id}", g.Manage(h.RemoveDomain)).Methods(http.MethodDelete)
	email.HandleFunc("/domains/{id}/recheck", g.Manage(h.RecheckDomain)).Methods(http.MethodPost)
	email.HandleFunc("/senders", g.View(h.ListSenders)).Methods(http.MethodGet)
	email.HandleFunc("/senders", g.Manage(h.CreateSender)).Methods(http.MethodPost)
	email.HandleFunc("/senders/{id}", g.Manage(h.UpdateSender)).Methods(http.MethodPatch)
	email.HandleFunc("/senders/{id}", g.Manage(h.DeleteSender)).Methods(http.MethodDelete)
	email.HandleFunc("/suppressions", g.View(h.ListSuppressions)).Methods(http.MethodGet)
	email.HandleFunc("/suppressions", g.Manage(h.AddSuppression)).Methods(http.MethodPost)
	email.HandleFunc("/suppressions/{address}", g.Manage(h.RemoveSuppression)).Methods(http.MethodDelete)
}

func RegisterAdminRoutes(admin *mux.Router, h *AdminHandler) {
	if admin == nil {
		panic("email http: admin routes need the router")
	}
	admin.HandleFunc("/admin/email/accounts", h.ListAccounts).Methods(http.MethodGet)
	admin.HandleFunc("/admin/email/quota", h.SendQuota).Methods(http.MethodGet)
	admin.HandleFunc("/admin/email/accounts/{workspaceId}/pause", h.Pause).Methods(http.MethodPost)
	admin.HandleFunc("/admin/email/accounts/{workspaceId}/reinstate", h.Reinstate).Methods(http.MethodPost)
	admin.HandleFunc("/admin/email/accounts/{workspaceId}/promote", h.Promote).Methods(http.MethodPost)
}
