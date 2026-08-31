package unofficial_whatsapp

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

// RegisterCampaignRoutes wires the campaign endpoints.
//
// The permission split is the point of this file. Campaigns are a SEPARATE
// resource from instances, because connecting a number and blasting from it are
// different privileges: an attendant trusted to reconnect a dropped session must
// not thereby be able to start a 40.000-number campaign from it.
//
// Within that resource the verbs follow the official campaign exactly —
// create / read / update / delete / start / stop — so an operator who knows one
// product's permissions knows the other's.
func RegisterCampaignRoutes(
	protected *mux.Router,
	h *CampaignHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	// A nil handler means campaigns are not wired; register nothing rather than
	// routes whose methods would nil-panic on the first request.
	if h == nil {
		return
	}

	res := workspace_domain.ResourceUnofficialWhatsAppCampaigns
	r := protected.PathPrefix("/unofficial-whatsapp/campaigns").Subrouter()

	// Reads.
	r.HandleFunc("", ac(res, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	r.HandleFunc("/archived", ac(res, workspace_domain.ActionRead, h.ListArchived)).Methods(http.MethodGet)
	r.HandleFunc("/summary", ac(res, workspace_domain.ActionRead, h.Summary)).Methods(http.MethodGet)
	r.HandleFunc("/{id}", ac(res, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	r.HandleFunc("/{id}/entries", ac(res, workspace_domain.ActionRead, h.ListEntries)).Methods(http.MethodGet)

	// Writes.
	r.HandleFunc("", ac(res, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	r.HandleFunc("/{id}", ac(res, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPut)
	r.HandleFunc("/{id}", ac(res, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
	r.HandleFunc("/{id}/department", ac(res, workspace_domain.ActionUpdate, h.AssignDepartment)).Methods(http.MethodPatch)
	r.HandleFunc("/{id}/archive", ac(res, workspace_domain.ActionUpdate, h.Archive)).Methods(http.MethodPatch)
	r.HandleFunc("/{id}/unarchive", ac(res, workspace_domain.ActionUpdate, h.Unarchive)).Methods(http.MethodPatch)

	// Entries.
	r.HandleFunc("/{id}/entries", ac(res, workspace_domain.ActionUpdate, h.AddEntries)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/entries/{entryId}", ac(res, workspace_domain.ActionUpdate, h.UpdateEntry)).Methods(http.MethodPatch)
	r.HandleFunc("/{id}/entries/{entryId}", ac(res, workspace_domain.ActionDelete, h.DeleteEntry)).Methods(http.MethodDelete)

	// Lifecycle. Start is ActionStart and both halts are ActionStop, matching the
	// official campaign: being able to halt a runaway blast is a safety valve and
	// must not require the privilege to launch one.
	r.HandleFunc("/{id}/start", ac(res, workspace_domain.ActionStart, h.Start)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/quick-send", ac(res, workspace_domain.ActionStart, h.QuickSend)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/pause", ac(res, workspace_domain.ActionStop, h.Pause)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/stop", ac(res, workspace_domain.ActionStop, h.Stop)).Methods(http.MethodPost)

	// The pre-flight list clean. ActionUpdate rather than ActionStart: it changes
	// entry statuses but sends nothing, so it must not require the privilege to
	// launch a campaign.
	r.HandleFunc("/{id}/validate", ac(res, workspace_domain.ActionUpdate, h.Validate)).Methods(http.MethodPost)

	// Destructive, both behind a confirmation code the prepare step issues.
	r.HandleFunc("/{id}/reset/prepare", ac(res, workspace_domain.ActionUpdate, h.PrepareReset)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/reset", ac(res, workspace_domain.ActionUpdate, h.ConfirmReset)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/clear-history/prepare", ac(res, workspace_domain.ActionUpdate, h.PrepareClearHistory)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/clear-history", ac(res, workspace_domain.ActionUpdate, h.ConfirmClearHistory)).Methods(http.MethodPost)
}
