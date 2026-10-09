package callhistory_usecase

import (
	"errors"
	"testing"

	"vozko/domain/callrouting"
	"vozko/domain/calls/cdr"
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
		{Resource: workspace.ResourceCallRecordings, Action: workspace.ActionRead}:    true,
	}, "ws1", "u1")
	if !manager.SeesEveryone || !manager.HearsRecordings {
		t.Fatalf("viewer with grants = %+v", manager)
	}
}

func TestAViewerSeesACallOnlyWhenTheyTookPartOrSeeTheTeam(t *testing.T) {
	agent := "u-agent"
	call := cdr.Call{CallID: "c1", Direction: cdr.DirectionOutbound, AgentID: &agent}
	transferred := []callrouting.TransferRecord{{CallID: "c1", FromUserID: agent, Target: callrouting.TransferTarget{UserID: "u-target"}}}
	cases := []struct {
		name      string
		viewer    Viewer
		transfers []callrouting.TransferRecord
		want      bool
	}{
		{"the agent who placed it", Viewer{UserID: agent}, nil, true},
		{"the colleague it was passed to", Viewer{UserID: "u-target"}, transferred, true},
		{"a colleague outside the call", Viewer{UserID: "u-other"}, transferred, false},
		{"a manager who sees the team", Viewer{UserID: "u-other", SeesEveryone: true}, nil, true},
		{"nobody signed in", Viewer{}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.viewer.Sees(call, tc.transfers); got != tc.want {
				t.Fatalf("Sees = %v, want %v", got, tc.want)
			}
		})
	}
}
