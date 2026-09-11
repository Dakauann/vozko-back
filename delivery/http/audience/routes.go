package audience

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

// RegisterProtectedRoutes wires the authenticated comment-analysis routes
// (plan §12). Reads are ActionRead; moderation, configuration, retries and
// backfills are ActionUpdate on the audience resource.
func RegisterProtectedRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	// A nil handler means the feature is not wired; register nothing rather
	// than routes whose methods would nil-panic on the first request.
	if h == nil {
		return
	}
	res := workspace_domain.ResourceAudience
	read, update := workspace_domain.ActionRead, workspace_domain.ActionUpdate
	// Forwarding puts a message on the workspace's own WhatsApp, to a real
	// person, so it is not the same privilege as configuring or moderating.
	send := workspace_domain.ActionSend

	r := protected.PathPrefix("/audience").Subrouter()

	r.HandleFunc("", ac(res, read, h.List)).Methods(http.MethodGet)
	r.HandleFunc("/stats", ac(res, read, h.Stats)).Methods(http.MethodGet)
	r.HandleFunc("/trends", ac(res, read, h.Trends)).Methods(http.MethodGet)
	r.HandleFunc("/spend", ac(res, read, h.Spend)).Methods(http.MethodGet)
	// How much of the rolling analysis budget the workspace has spent. Read
	// permission, like every other number on the dashboard it sits beside.
	r.HandleFunc("/usage", ac(res, read, h.Usage)).Methods(http.MethodGet)
	// What the workspace decides about its own analysis: the ceiling the budget
	// above is measured against, and how long a conversation must go quiet
	// before it is judged. Reading is a dashboard read; changing either is a
	// workspace-wide act that governs what every channel spends and when every
	// conversation is analysed, so it carries update.
	//
	// Not the workspace-config routes, which hold the same database row: those
	// are gated on admin-or-owner, and analysis is gated on audience:update.
	r.HandleFunc("/workspace-settings", ac(res, read, h.WorkspaceSettings)).Methods(http.MethodGet)
	r.HandleFunc("/workspace-settings", ac(res, update, h.UpdateWorkspaceSettings)).Methods(http.MethodPut)
	// The recipient picker is part of forwarding, so it carries forwarding's
	// permission: someone who may not send has no business enumerating who
	// the workspace talks to.
	r.HandleFunc("/escalation-recipients", ac(res, send, h.EscalationRecipients)).Methods(http.MethodGet)

	r.HandleFunc("/authors", ac(res, read, h.ListAuthors)).Methods(http.MethodGet)
	r.HandleFunc("/authors/{id}", ac(res, read, h.GetAuthor)).Methods(http.MethodGet)
	// The inverse of the feed's container filter: which posts one person turns
	// up on. A read of the same rows the feed reads, so the same read action.
	r.HandleFunc("/authors/{id}/containers", ac(res, read, h.ListAuthorContainers)).Methods(http.MethodGet)
	r.HandleFunc("/authors/{id}", ac(res, update, h.SetModeration)).Methods(http.MethodPatch)

	r.HandleFunc("/settings", ac(res, read, h.ListAccountSettings)).Methods(http.MethodGet)
	r.HandleFunc("/settings/{source}/{accountId}", ac(res, read, h.GetSettings)).Methods(http.MethodGet)
	r.HandleFunc("/settings/{source}/{accountId}", ac(res, update, h.UpdateSettings)).Methods(http.MethodPatch)
	// A post's own settings, layered over the account's.
	r.HandleFunc("/settings/{source}/{accountId}/containers/{containerId}", ac(res, read, h.GetContainerSettings)).Methods(http.MethodGet)
	r.HandleFunc("/settings/{source}/{accountId}/containers/{containerId}", ac(res, update, h.PutContainerSettings)).Methods(http.MethodPut)
	r.HandleFunc("/settings/{source}/{accountId}/containers/{containerId}", ac(res, update, h.DeleteContainerSettings)).Methods(http.MethodDelete)

	// Backfill: the two account-scoped routes before the id-scoped ones, so
	// "{source}/{accountId}/estimate" never matches "{id}".
	r.HandleFunc("/backfill/{source}/{accountId}/estimate", ac(res, read, h.EstimateBackfill)).Methods(http.MethodGet)
	r.HandleFunc("/backfill/{source}/{accountId}", ac(res, update, h.StartBackfill)).Methods(http.MethodPost)
	r.HandleFunc("/backfill/{id}/cancel", ac(res, update, h.CancelBackfill)).Methods(http.MethodPost)
	r.HandleFunc("/backfill/{id}", ac(res, read, h.GetBackfill)).Methods(http.MethodGet)

	r.HandleFunc("/{id}/retry", ac(res, update, h.Retry)).Methods(http.MethodPost)
	// Alerts. Behind SEND rather than update, because arming an automated
	// sender is granting sends: someone who may tune the classifier must not
	// thereby be able to message a phone number in the workspace's name.
	//
	// Registered BEFORE the "/{id}/..." routes below, or "/alerts/options"
	// would be matched as a comment id.
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
