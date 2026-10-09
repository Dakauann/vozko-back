package callhistory_usecase

import (
	"vozko/domain/callrouting"
	"vozko/domain/calls/callhistory"
	"vozko/domain/calls/cdr"
	"vozko/domain/workspace"
)

type Permissions interface {
	Execute(userID, workspaceID string, resource workspace.Resource, action workspace.Action) error
}

func ViewerFor(permissions Permissions, workspaceID, userID string) Viewer {
	may := func(resource workspace.Resource, action workspace.Action) bool {
		return permissions.Execute(userID, workspaceID, resource, action) == nil
	}
	return Viewer{
		WorkspaceID:     workspaceID,
		UserID:          userID,
		SeesEveryone:    may(workspace.ResourceCallHistory, workspace.ActionViewOthers),
		HearsRecordings: may(workspace.ResourceCallRecordings, workspace.ActionRead),
	}
}

func (v Viewer) Sees(call cdr.Call, transfers []callrouting.TransferRecord) bool {
	return callhistory.Visible(v.UserID, v.SeesEveryone, call, transfers)
}
