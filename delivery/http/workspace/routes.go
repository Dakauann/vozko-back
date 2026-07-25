package workspace

import (
	"net/http"

	"github.com/gorilla/mux"

	workspacedomain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

func RegisterProtectedRoutes(
	protected *mux.Router,
	h *WorkspaceHandler,
	ac func(workspacedomain.Resource, workspacedomain.Action, http.HandlerFunc) http.HandlerFunc,
	wsMW *middleware.WorkspaceMiddleware,
) {
	protected.HandleFunc("/workspaces", h.Create).Methods(http.MethodPost)
	protected.HandleFunc("/workspaces", h.List).Methods(http.MethodGet)
	protected.HandleFunc("/workspaces/invites", h.ListMyInvites).Methods(http.MethodGet)
	protected.HandleFunc("/workspaces/invites/accept", h.AcceptInvite).Methods(http.MethodPost)
	protected.HandleFunc("/workspaces/invites/{inviteId}/decline", h.DeclineInvite).Methods(http.MethodPost)
	protected.HandleFunc("/workspaces/permissions", h.ListResourcePermissions).Methods(http.MethodGet)
	protected.HandleFunc("/workspaces/{workspaceId}", h.Get).Methods(http.MethodGet)
	protected.HandleFunc("/workspaces/{workspaceId}", h.Update).Methods(http.MethodPut)

	mb := workspacedomain.ResourceMembers
	protected.HandleFunc("/workspaces/{workspaceId}/members", ac(mb, workspacedomain.ActionRead, h.ListMembers)).Methods(http.MethodGet)
	protected.HandleFunc("/workspaces/{workspaceId}/members/assignable", ac(mb, workspacedomain.ActionRead, h.ListAssignableMembers)).Methods(http.MethodGet)

	protected.HandleFunc("/workspaces/{workspaceId}/members/{userId}/permissions", h.GetMemberPermissions).Methods(http.MethodGet)
	protected.HandleFunc("/workspaces/{workspaceId}/invites", ac(mb, workspacedomain.ActionRead, h.ListWorkspaceInvites)).Methods(http.MethodGet)
	protected.HandleFunc("/workspaces/{workspaceId}/invites/{inviteId}", ac(mb, workspacedomain.ActionDelete, h.CancelInvite)).Methods(http.MethodDelete)
	protected.HandleFunc("/workspaces/{workspaceId}/members/invite", ac(mb, workspacedomain.ActionCreate, h.InviteMember)).Methods(http.MethodPost)
	protected.HandleFunc("/workspaces/{workspaceId}/members/{userId}/role", ac(mb, workspacedomain.ActionUpdate, h.UpdateMemberRole)).Methods(http.MethodPut)
	protected.HandleFunc("/workspaces/{workspaceId}/members/{userId}/permissions", ac(mb, workspacedomain.ActionUpdate, h.SetMemberPermissions)).Methods(http.MethodPut)
	protected.HandleFunc("/workspaces/{workspaceId}/members/{userId}", ac(mb, workspacedomain.ActionDelete, h.RemoveMember)).Methods(http.MethodDelete)
	protected.HandleFunc("/workspaces/{workspaceId}/members/{userId}/assign-role", ac(mb, workspacedomain.ActionUpdate, h.AssignCustomRole)).Methods(http.MethodPut)

	rl := workspacedomain.ResourceRoles
	protected.HandleFunc("/workspaces/{workspaceId}/roles", ac(rl, workspacedomain.ActionRead, h.ListCustomRoles)).Methods(http.MethodGet)
	protected.HandleFunc("/workspaces/{workspaceId}/roles", ac(rl, workspacedomain.ActionCreate, h.CreateCustomRole)).Methods(http.MethodPost)
	protected.HandleFunc("/workspaces/{workspaceId}/roles/{roleId}", ac(rl, workspacedomain.ActionUpdate, h.UpdateCustomRole)).Methods(http.MethodPut)
	protected.HandleFunc("/workspaces/{workspaceId}/roles/{roleId}", ac(rl, workspacedomain.ActionDelete, h.DeleteCustomRole)).Methods(http.MethodDelete)

	assignmentRouter := protected.PathPrefix("/workspaces/{workspaceId}").Subrouter()
	assignmentRouter.Use(wsMW.RequireAccess(workspacedomain.ResourceAssignments, workspacedomain.ActionCreate))
	assignmentRouter.HandleFunc("/assignments", h.AssignResource).Methods(http.MethodPost)

	unassignmentRouter := protected.PathPrefix("/workspaces/{workspaceId}").Subrouter()
	unassignmentRouter.Use(wsMW.RequireAccess(workspacedomain.ResourceAssignments, workspacedomain.ActionDelete))
	unassignmentRouter.HandleFunc("/assignments/{resourceType}/{resourceId}/members/{userId}", h.UnassignResource).Methods(http.MethodDelete)

	listAssignmentRouter := protected.PathPrefix("/workspaces/{workspaceId}").Subrouter()
	listAssignmentRouter.Use(wsMW.RequireMembership())
	listAssignmentRouter.HandleFunc("/assignments/{resourceType}/{resourceId}", h.ListResourceAssignments).Methods(http.MethodGet)
}

func RegisterAdminRoutes(adminRoutes *mux.Router, h *WorkspaceHandler) {
	adminRoutes.HandleFunc("/admin/workspaces/{workspaceId}/members-paginated", h.ListMembersPaginated).Methods(http.MethodGet)
}
