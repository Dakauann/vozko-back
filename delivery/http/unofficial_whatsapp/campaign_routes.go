package unofficial_whatsapp

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterCampaignRoutes(
	protected *mux.Router,
	h *CampaignHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		return
	}

	res := workspace_domain.ResourceUnofficialWhatsAppCampaigns
	r := protected.PathPrefix("/unofficial-whatsapp/campaigns").Subrouter()

	r.HandleFunc("", ac(res, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	r.HandleFunc("/archived", ac(res, workspace_domain.ActionRead, h.ListArchived)).Methods(http.MethodGet)
	r.HandleFunc("/summary", ac(res, workspace_domain.ActionRead, h.Summary)).Methods(http.MethodGet)
	r.HandleFunc("/{id}", ac(res, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	r.HandleFunc("/{id}/entries", ac(res, workspace_domain.ActionRead, h.ListEntries)).Methods(http.MethodGet)

	r.HandleFunc("", ac(res, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	r.HandleFunc("/{id}", ac(res, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPut)
	r.HandleFunc("/{id}", ac(res, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
	r.HandleFunc("/{id}/department", ac(res, workspace_domain.ActionUpdate, h.AssignDepartment)).Methods(http.MethodPatch)
	r.HandleFunc("/{id}/archive", ac(res, workspace_domain.ActionUpdate, h.Archive)).Methods(http.MethodPatch)
	r.HandleFunc("/{id}/unarchive", ac(res, workspace_domain.ActionUpdate, h.Unarchive)).Methods(http.MethodPatch)

	r.HandleFunc("/{id}/entries", ac(res, workspace_domain.ActionUpdate, h.AddEntries)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/entries/{entryId}", ac(res, workspace_domain.ActionUpdate, h.UpdateEntry)).Methods(http.MethodPatch)
	r.HandleFunc("/{id}/entries/{entryId}", ac(res, workspace_domain.ActionDelete, h.DeleteEntry)).Methods(http.MethodDelete)

	r.HandleFunc("/{id}/start", ac(res, workspace_domain.ActionStart, h.Start)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/quick-send", ac(res, workspace_domain.ActionStart, h.QuickSend)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/pause", ac(res, workspace_domain.ActionStop, h.Pause)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/stop", ac(res, workspace_domain.ActionStop, h.Stop)).Methods(http.MethodPost)

	r.HandleFunc("/{id}/validate", ac(res, workspace_domain.ActionUpdate, h.Validate)).Methods(http.MethodPost)

	r.HandleFunc("/{id}/reset/prepare", ac(res, workspace_domain.ActionUpdate, h.PrepareReset)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/reset", ac(res, workspace_domain.ActionUpdate, h.ConfirmReset)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/clear-history/prepare", ac(res, workspace_domain.ActionUpdate, h.PrepareClearHistory)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/clear-history", ac(res, workspace_domain.ActionUpdate, h.ConfirmClearHistory)).Methods(http.MethodPost)
}
