package callrouting

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(protected *mux.Router, h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc) {
	if h == nil {
		return
	}
	res := workspace_domain.ResourceCallQueues
	queues := protected.PathPrefix("/call-queues").Subrouter()
	queues.HandleFunc("/transfer-targets", ac(workspace_domain.ResourceCallSession, workspace_domain.ActionUse, h.ListTransferTargets)).Methods(http.MethodGet)
	queues.HandleFunc("/live", ac(res, workspace_domain.ActionRead, h.LiveQueues)).Methods(http.MethodGet)
	queues.HandleFunc("/stats", ac(res, workspace_domain.ActionRead, h.QueueStats)).Methods(http.MethodGet)
	queues.HandleFunc("", ac(res, workspace_domain.ActionCreate, h.CreateQueue)).Methods(http.MethodPost)
	queues.HandleFunc("", ac(res, workspace_domain.ActionRead, h.ListQueues)).Methods(http.MethodGet)
	queues.HandleFunc("/{id}", ac(res, workspace_domain.ActionRead, h.GetQueue)).Methods(http.MethodGet)
	queues.HandleFunc("/{id}", ac(res, workspace_domain.ActionUpdate, h.UpdateQueue)).Methods(http.MethodPut)
	queues.HandleFunc("/{id}", ac(res, workspace_domain.ActionDelete, h.DeleteQueue)).Methods(http.MethodDelete)

	protected.HandleFunc("/call-routing/settings", ac(res, workspace_domain.ActionRead, h.GetSettings)).Methods(http.MethodGet)
	protected.HandleFunc("/call-routing/settings", ac(res, workspace_domain.ActionUpdate, h.SaveSettings)).Methods(http.MethodPut)
	protected.HandleFunc("/hold-music/presets", ac(res, workspace_domain.ActionRead, h.ListPresets)).Methods(http.MethodGet)
	protected.HandleFunc("/hold-music/presets/{id}/audio", ac(res, workspace_domain.ActionRead, h.PresetAudio)).Methods(http.MethodGet)
}
