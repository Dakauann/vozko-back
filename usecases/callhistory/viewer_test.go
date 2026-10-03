package callhistory_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workspace"
)

type grants map[workspace.PermissionEntry]bool

func (g grants) Execute(_, _ string, resource workspace.Resource, action workspace.Action) error {
	if g[workspace.PermissionEntry{Resource: resource, Action: action}] {
		return nil
	}
	return errors.New("denied")
}

func TestTheViewerSeesTheTeamAndHearsRecordingsOnlyWithThoseGrants(t *testing.T) {
	plain := ViewerFor(grants{}, "ws1", "u1")
	if plain.WorkspaceID != "ws1" || plain.UserID != "u1" || plain.SeesEveryone || plain.HearsRecordings {
		t.Fatalf("viewer without grants = %+v", plain)
	}
	manager := ViewerFor(grants{
		{Resource: workspace.ResourceCallHistory, Action: workspace.ActionViewOthers}: true,
		{Resource: workspace.ResourceCallRecordings, Action: workspace.ActionRead}:     true,
	}, "ws1", "u1")
	if !manager.SeesEveryone || !manager.HearsRecordings {
		t.Fatalf("viewer with grants = %+v", manager)
	}
}
