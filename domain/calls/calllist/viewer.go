package calllist

import "vozko/domain/workspace"

type Viewer struct {
	UserID  string
	Views   bool
	Manages bool
}

func Holds(checker workspace.PermissionChecker, workspaceID, userID string, isAdmin bool, key workspace.CapabilityKey) bool {
	entries, err := workspace.CapabilitiesRequire([]workspace.CapabilityKey{key})
	if err != nil {
		return false
	}
	return workspace.HoldsAll(checker, workspaceID, userID, isAdmin, entries)
}

func ViewerOf(checker workspace.PermissionChecker, workspaceID, userID string, isAdmin bool) Viewer {
	return Viewer{
		UserID:  userID,
		Views:   Holds(checker, workspaceID, userID, isAdmin, CapabilityView),
		Manages: Holds(checker, workspaceID, userID, isAdmin, CapabilityManage),
	}
}

func (v Viewer) Sees(assignees []string) bool {
	return v.Views && (&List{AssigneeIDs: assignees}).VisibleTo(v.UserID, v.Manages)
}
