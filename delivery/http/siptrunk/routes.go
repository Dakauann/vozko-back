package siptrunk

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *SIPTrunkHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	st := workspace_domain.ResourceSIPTrunks

	protected.HandleFunc("/sip-trunks", ac(st, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/sip-trunks/codecs", h.GetSupportedCodecs).Methods(http.MethodGet)
	protected.HandleFunc("/sip-trunks/{trunkId}", ac(st, workspace_domain.ActionRead, h.Get)).Methods(http.MethodGet)
	protected.HandleFunc("/sip-trunks/{trunkId}/status", ac(st, workspace_domain.ActionRead, h.GetStatus)).Methods(http.MethodGet)

	protected.HandleFunc("/sip-trunks", ac(st, workspace_domain.ActionCreate, h.CreateForWorkspace)).Methods(http.MethodPost)
	protected.HandleFunc("/sip-trunks/{trunkId}", ac(st, workspace_domain.ActionUpdate, h.UpdateForWorkspace)).Methods(http.MethodPut)
	protected.HandleFunc("/sip-trunks/{trunkId}", ac(st, workspace_domain.ActionDelete, h.DeleteForWorkspace)).Methods(http.MethodDelete)
	protected.HandleFunc("/sip-trunks/{trunkId}/enable", ac(st, workspace_domain.ActionUpdate, h.EnableForWorkspace)).Methods(http.MethodPut)
}

func RegisterAdminRoutes(
	adminRoutes *mux.Router,
	h *SIPTrunkHandler,
) {
	adminRoutes.HandleFunc("/admin/sip-trunks", h.Create).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/admin/sip-trunks/{trunkId}", h.Get).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/sip-trunks/{trunkId}", h.Update).Methods(http.MethodPut)
	adminRoutes.HandleFunc("/admin/sip-trunks/{trunkId}", h.Delete).Methods(http.MethodDelete)
	adminRoutes.HandleFunc("/admin/sip-trunks/{trunkId}/enable", h.Enable).Methods(http.MethodPut)
	adminRoutes.HandleFunc("/admin/sip-trunks/{trunkId}/status", h.GetStatus).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/sip-trunks/{trunkId}/owner", h.AssignOwner).Methods(http.MethodPatch)
}
