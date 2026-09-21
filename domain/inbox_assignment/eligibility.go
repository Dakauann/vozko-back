package inbox_assignment

const (
	RouletteResource = "conversations"
	RouletteAction   = "roulette"
)

type RoulettePermissionChecker interface {
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
	IsWorkspaceOwnerOrAdmin(userID, workspaceID string) bool
}

func CanReceiveRoulette(authz RoulettePermissionChecker, userID, workspaceID string, skipAdmins bool) bool {
	if authz == nil || userID == "" || workspaceID == "" {
		return false
	}
	if skipAdmins && authz.IsWorkspaceOwnerOrAdmin(userID, workspaceID) {
		return false
	}
	return authz.HasWorkspacePermission(userID, workspaceID, RouletteResource, RouletteAction, false)
}
