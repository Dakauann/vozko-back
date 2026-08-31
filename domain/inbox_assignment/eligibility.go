package inbox_assignment

// The permission a member must hold to be handed a conversation by the
// roulette. Spelled out here rather than at each call site so the two pools
// below cannot end up asking for different permissions.
const (
	RouletteResource = "conversations"
	RouletteAction   = "roulette"
)

// RoulettePermissionChecker is the slice of the conversation authorizer the
// roulette needs. Satisfied by infra/conversation.Authorizer through
// conversation.ConversationAuthorizer.
type RoulettePermissionChecker interface {
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
	IsWorkspaceOwnerOrAdmin(userID, workspaceID string) bool
}

// CanReceiveRoulette is THE answer to "may this member be handed a conversation
// by the roulette".
//
// It lives in the domain, not in the WS hub, because there are now two pools
// that must agree: the connected pool (delivery/ws, from live sockets) and the
// last-seen pool (usecases, from workspace membership). A member the hub would
// route to but the roster would not — or the reverse — is a distribution bug
// invisible from either side, since neither pool can see the other's answer.
//
// skipAdmins is the workspace's SkipAdminAssignment flag: when set, owners and
// admins do not take part in automatic distribution.
func CanReceiveRoulette(authz RoulettePermissionChecker, userID, workspaceID string, skipAdmins bool) bool {
	if authz == nil || userID == "" || workspaceID == "" {
		return false
	}
	if skipAdmins && authz.IsWorkspaceOwnerOrAdmin(userID, workspaceID) {
		return false
	}
	return authz.HasWorkspacePermission(userID, workspaceID, RouletteResource, RouletteAction, false)
}
