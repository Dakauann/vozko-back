package carriercampaign

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

type AccessControl func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc

func RegisterProtectedRoutes(protected *mux.Router, h *Handler, ac AccessControl) {
	if h == nil || ac == nil {
		return
	}
	guard := func(action workspace_domain.Action) func(http.HandlerFunc) http.HandlerFunc {
		return func(f http.HandlerFunc) http.HandlerFunc {
			return ac(workspace_domain.ResourceCarrierMessaging, action, f)
		}
	}
	read := guard(workspace_domain.ActionRead)
	create := guard(workspace_domain.ActionCreate)
	update := guard(workspace_domain.ActionUpdate)
	remove := guard(workspace_domain.ActionDelete)
	start := guard(workspace_domain.ActionStart)
	stop := guard(workspace_domain.ActionStop)

	campaigns := protected.PathPrefix("/carrier/campaigns").Subrouter()
	campaigns.HandleFunc("", read(h.List)).Methods(http.MethodGet)
	campaigns.HandleFunc("", create(h.Create)).Methods(http.MethodPost)
	campaigns.HandleFunc("/{id}", read(h.Get)).Methods(http.MethodGet)
	campaigns.HandleFunc("/{id}", update(h.Update)).Methods(http.MethodPatch)
	campaigns.HandleFunc("/{id}", remove(h.Delete)).Methods(http.MethodDelete)
	campaigns.HandleFunc("/{id}/duplicate", create(h.Duplicate)).Methods(http.MethodPost)
	campaigns.HandleFunc("/{id}/estimate", update(h.Estimate)).Methods(http.MethodPost)
	campaigns.HandleFunc("/{id}/test", update(h.TestSend)).Methods(http.MethodPost)
	campaigns.HandleFunc("/{id}/launch", start(h.Launch)).Methods(http.MethodPost)
	campaigns.HandleFunc("/{id}/ready", start(h.Ready)).Methods(http.MethodPost)
	campaigns.HandleFunc("/{id}/resume", start(h.Resume)).Methods(http.MethodPost)
	campaigns.HandleFunc("/{id}/pause", stop(h.Pause)).Methods(http.MethodPost)
	campaigns.HandleFunc("/{id}/stop", stop(h.Stop)).Methods(http.MethodPost)
	campaigns.HandleFunc("/{id}/funnel", read(h.Funnel)).Methods(http.MethodGet)
	campaigns.HandleFunc("/{id}/variants", read(h.Variants)).Methods(http.MethodGet)
	campaigns.HandleFunc("/{id}/buttons", read(h.Buttons)).Methods(http.MethodGet)
	campaigns.HandleFunc("/{id}/carriers", read(h.Carriers)).Methods(http.MethodGet)
	campaigns.HandleFunc("/{id}/timeline", read(h.Timeline)).Methods(http.MethodGet)
	campaigns.HandleFunc("/{id}/recipients", read(h.Recipients)).Methods(http.MethodGet)

	protected.HandleFunc("/carrier/overview", read(h.Overview)).Methods(http.MethodGet)
}
