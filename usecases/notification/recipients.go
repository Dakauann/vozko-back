package notification_usecase

import (
	"vozko/domain/notification"
	"vozko/domain/workspace"
)

type recipientSelector struct {
	permissions workspace.PermissionChecker
}

func (s recipientSelector) choose(workspaceID string, audience notification.Audience, members []notification.Member) []notification.Member {
	return audience.Select(members, s.holds(workspaceID, audience))
}

func (s recipientSelector) holds(workspaceID string, audience notification.Audience) func(string) bool {
	permission := audience.Permission
	if s.permissions == nil || permission == nil {
		return nil
	}
	return func(userID string) bool {
		return s.permissions.HasWorkspacePermission(userID, workspaceID, string(permission.Resource), string(permission.Action), false)
	}
}

func memberIDs(members []notification.Member) []string {
	ids := make([]string, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.UserID)
	}
	return ids
}
