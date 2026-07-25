package dialerringchannels

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *DialerRingChannelsHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	dl := workspace_domain.ResourceDialer
	protected.HandleFunc("/dialer/ring-channels", ac(dl, workspace_domain.ActionUse, h.GetMine)).Methods(http.MethodGet)
	protected.HandleFunc("/dialer/ring-channels", ac(dl, workspace_domain.ActionUse, h.SetMine)).Methods(http.MethodPut)
}
