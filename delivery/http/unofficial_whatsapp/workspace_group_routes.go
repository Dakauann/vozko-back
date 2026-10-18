package unofficial_whatsapp

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterWorkspaceGroupRoutes(
	protected *mux.Router,
	h *WorkspaceGroupHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		return
	}

	res := workspace_domain.ResourceUnofficialWhatsAppGroups
	read, create, update := workspace_domain.ActionRead, workspace_domain.ActionCreate, workspace_domain.ActionUpdate
	remove, export, configure := workspace_domain.ActionDelete, workspace_domain.ActionExport, workspace_domain.ActionConfigure
	get, post, patch, del := http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete
	r := protected.PathPrefix("/unofficial-whatsapp/groups").Subrouter()

	r.HandleFunc("", ac(res, read, h.List)).Methods(get)
	r.HandleFunc("", ac(res, create, h.Create)).Methods(post)
	r.HandleFunc("/dashboard", ac(res, read, h.Dashboard)).Methods(get)
	r.HandleFunc("/membership", ac(res, read, h.Membership)).Methods(get)
	r.HandleFunc("/people", ac(res, read, h.People)).Methods(get)
	r.HandleFunc("/themes", ac(res, read, h.Themes)).Methods(get)
	r.HandleFunc("/categories", ac(res, read, h.Categories)).Methods(get)
	r.HandleFunc("/discover", ac(res, update, h.Discover)).Methods(post)

	r.HandleFunc("/invites/preview", ac(res, create, h.PreviewInvite)).Methods(post)
	r.HandleFunc("/joins", ac(res, read, h.Joins)).Methods(get)
	r.HandleFunc("/joins", ac(res, create, h.Join)).Methods(post)
	r.HandleFunc("/joins/{planId}/cancel", ac(res, create, h.CancelJoin)).Methods(post)

	r.HandleFunc("/analyses", ac(res, read, h.ListAnalyses)).Methods(get)
	r.HandleFunc("/analyses", ac(res, configure, h.CreateAnalysis)).Methods(post)
	r.HandleFunc("/analyses/{id}", ac(res, read, h.GetAnalysis)).Methods(get)
	r.HandleFunc("/analyses/{id}", ac(res, configure, h.UpdateAnalysis)).Methods(patch)
	r.HandleFunc("/analyses/{id}", ac(res, remove, h.DeleteAnalysis)).Methods(del)
	r.HandleFunc("/analyses/{id}/runs", ac(res, read, h.AnalysisRuns)).Methods(get)
	r.HandleFunc("/analyses/{id}/runs/{runId}/messages", ac(res, read, h.RunMessages)).Methods(get)

	r.HandleFunc("/{id}", ac(res, read, h.Get)).Methods(get)
	r.HandleFunc("/{id}", ac(res, update, h.Update)).Methods(patch)
	r.HandleFunc("/{id}/follow", ac(res, update, h.Follow)).Methods(post)
	r.HandleFunc("/{id}/unfollow", ac(res, update, h.Unfollow)).Methods(post)
	r.HandleFunc("/{id}/refresh", ac(res, update, h.Refresh)).Methods(post)
	r.HandleFunc("/{id}/series", ac(res, create, h.NextInSeries)).Methods(post)
	r.HandleFunc("/{id}/leave", ac(res, remove, h.Leave)).Methods(post)
	r.HandleFunc("/{id}/participants", ac(res, update, h.ManageMembers)).Methods(post)
	r.HandleFunc("/{id}/participants", ac(res, remove, h.ManageMembers)).Methods(del)
	r.HandleFunc("/{id}/invite-link", ac(res, read, h.InviteLink)).Methods(get)
	r.HandleFunc("/{id}/campaigns", ac(res, read, h.Campaigns)).Methods(get)
	r.HandleFunc("/{id}/members", ac(res, read, h.Members)).Methods(get)
	r.HandleFunc("/{id}/members/export", ac(res, export, h.ExportMembers)).Methods(get)
	r.HandleFunc("/{id}/members/leads", ac(res, read,
		ac(workspace_domain.ResourceLeads, read, ac(workspace_domain.ResourceLeads, create, h.MembersToLeads)))).Methods(post)
}
