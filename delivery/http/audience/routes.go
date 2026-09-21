package audience

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		return
	}
	res := workspace_domain.ResourceAudience
	read, update := workspace_domain.ActionRead, workspace_domain.ActionUpdate
	send := workspace_domain.ActionSend

	r := protected.PathPrefix("/audience").Subrouter()

	r.HandleFunc("", ac(res, read, h.List)).Methods(http.MethodGet)
	r.HandleFunc("/stats", ac(res, read, h.Stats)).Methods(http.MethodGet)
	r.HandleFunc("/trends", ac(res, read, h.Trends)).Methods(http.MethodGet)
	r.HandleFunc("/spend", ac(res, read, h.Spend)).Methods(http.MethodGet)
	r.HandleFunc("/usage", ac(res, read, h.Usage)).Methods(http.MethodGet)
	r.HandleFunc("/workspace-settings", ac(res, read, h.WorkspaceSettings)).Methods(http.MethodGet)
	r.HandleFunc("/workspace-settings", ac(res, update, h.UpdateWorkspaceSettings)).Methods(http.MethodPut)
	r.HandleFunc("/escalation-recipients", ac(res, send, h.EscalationRecipients)).Methods(http.MethodGet)

	r.HandleFunc("/authors", ac(res, read, h.ListAuthors)).Methods(http.MethodGet)
	r.HandleFunc("/authors/{id}", ac(res, read, h.GetAuthor)).Methods(http.MethodGet)
	r.HandleFunc("/authors/{id}/containers", ac(res, read, h.ListAuthorContainers)).Methods(http.MethodGet)
	r.HandleFunc("/authors/{id}", ac(res, update, h.SetModeration)).Methods(http.MethodPatch)

	r.HandleFunc("/settings", ac(res, read, h.ListAccountSettings)).Methods(http.MethodGet)
	r.HandleFunc("/settings/{source}/{accountId}", ac(res, read, h.GetSettings)).Methods(http.MethodGet)
	r.HandleFunc("/settings/{source}/{accountId}", ac(res, update, h.UpdateSettings)).Methods(http.MethodPatch)
	r.HandleFunc("/settings/{source}/{accountId}/containers/{containerId}", ac(res, read, h.GetContainerSettings)).Methods(http.MethodGet)
	r.HandleFunc("/settings/{source}/{accountId}/containers/{containerId}", ac(res, update, h.PutContainerSettings)).Methods(http.MethodPut)
	r.HandleFunc("/settings/{source}/{accountId}/containers/{containerId}", ac(res, update, h.DeleteContainerSettings)).Methods(http.MethodDelete)

	r.HandleFunc("/backfill/{source}/{accountId}/estimate", ac(res, read, h.EstimateBackfill)).Methods(http.MethodGet)
	r.HandleFunc("/backfill/{source}/{accountId}", ac(res, update, h.StartBackfill)).Methods(http.MethodPost)
	r.HandleFunc("/backfill/{id}/cancel", ac(res, update, h.CancelBackfill)).Methods(http.MethodPost)
	r.HandleFunc("/backfill/{id}", ac(res, read, h.GetBackfill)).Methods(http.MethodGet)

	r.HandleFunc("/{id}/retry", ac(res, update, h.Retry)).Methods(http.MethodPost)
	r.HandleFunc("/alerts", ac(res, send, h.ListAlertRules)).Methods(http.MethodGet)
	r.HandleFunc("/alerts", ac(res, send, h.CreateAlertRule)).Methods(http.MethodPost)
	r.HandleFunc("/alerts/options", ac(res, send, h.AlertOptions)).Methods(http.MethodGet)
	r.HandleFunc("/alerts/{id}", ac(res, send, h.UpdateAlertRule)).Methods(http.MethodPut)
	r.HandleFunc("/alerts/{id}", ac(res, send, h.DeleteAlertRule)).Methods(http.MethodDelete)
	r.HandleFunc("/alerts/{id}/test", ac(res, send, h.TestAlertRule)).Methods(http.MethodPost)

	r.HandleFunc("/{id}/escalate", ac(res, send, h.Escalate)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/reply/suggest", ac(res, send, h.SuggestReply)).Methods(http.MethodPost)
	r.HandleFunc("/{id}/reply", ac(res, send, h.PostReply)).Methods(http.MethodPost)
}
