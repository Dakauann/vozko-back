package branch

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *BranchHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	br := workspace_domain.ResourceBranches

	protected.HandleFunc("/branches/mine", ac(br, workspace_domain.ActionUse, h.ListMine)).Methods(http.MethodGet)
	protected.HandleFunc("/branches/mine/{branchId}/rotate-secret", ac(br, workspace_domain.ActionUse, h.RotateMineSecret)).Methods(http.MethodPost)

	protected.HandleFunc("/branches/sip-config", ac(br, workspace_domain.ActionUse, h.SIPConfig)).Methods(http.MethodGet)

	protected.HandleFunc("/branches", ac(br, workspace_domain.ActionRead, h.ListForWorkspace)).Methods(http.MethodGet)
	protected.HandleFunc("/branches", ac(br, workspace_domain.ActionCreate, h.CreateForWorkspace)).Methods(http.MethodPost)
	protected.HandleFunc("/branches/{branchId}", ac(br, workspace_domain.ActionRead, h.GetForWorkspace)).Methods(http.MethodGet)
	protected.HandleFunc("/branches/{branchId}", ac(br, workspace_domain.ActionUpdate, h.UpdateForWorkspace)).Methods(http.MethodPut)
	protected.HandleFunc("/branches/{branchId}", ac(br, workspace_domain.ActionDelete, h.DeleteForWorkspace)).Methods(http.MethodDelete)
	protected.HandleFunc("/branches/{branchId}/enable", ac(br, workspace_domain.ActionUpdate, h.EnableForWorkspace)).Methods(http.MethodPut)
	protected.HandleFunc("/branches/{branchId}/rotate-secret", ac(br, workspace_domain.ActionUpdate, h.RotateSecretForWorkspace)).Methods(http.MethodPost)
}

func RegisterAdminRoutes(
	adminRoutes *mux.Router,
	h *BranchHandler,
) {
	adminRoutes.HandleFunc("/admin/workspaces/{workspaceId}/branches", h.AdminListByWorkspace).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/workspaces/{workspaceId}/branches", h.AdminCreate).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/admin/branches/{branchId}", h.AdminGet).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/branches/{branchId}", h.AdminUpdate).Methods(http.MethodPut)
	adminRoutes.HandleFunc("/admin/branches/{branchId}", h.AdminDelete).Methods(http.MethodDelete)
	adminRoutes.HandleFunc("/admin/branches/{branchId}/enable", h.AdminEnable).Methods(http.MethodPut)
	adminRoutes.HandleFunc("/admin/branches/{branchId}/rotate-secret", h.AdminRotateSecret).Methods(http.MethodPost)
}
