package calllist

import (
	"testing"

	"vozko/domain/workspace"
)

type permissionSet map[string]bool

func (p permissionSet) HasWorkspacePermission(_, _, resource, action string, _ bool) bool {
	return p[resource+":"+action]
}

func TestAListIsSeenByItsAssigneesAndByWhoManagesLists(t *testing.T) {
	cases := []struct {
		name      string
		viewer    Viewer
		assignees []string
		want      bool
	}{
		{"an assignee who views lists", Viewer{UserID: "u-1", Views: true}, []string{"u-2", "u-1"}, true},
		{"a member outside the list", Viewer{UserID: "u-3", Views: true}, []string{"u-1"}, false},
		{"a manager outside the list", Viewer{UserID: "u-3", Views: true, Manages: true}, []string{"u-1"}, true},
		{"an assignee who lost call_lists:read", Viewer{UserID: "u-1"}, []string{"u-1"}, false},
		{"a manager who lost call_lists:read", Viewer{UserID: "u-3", Manages: true}, []string{"u-1"}, false},
		{"a viewer with no user", Viewer{Views: true}, []string{""}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.viewer.Sees(tc.assignees); got != tc.want {
				t.Fatalf("Sees = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAListViewerHoldsTheListCapabilities(t *testing.T) {
	read := string(workspace.ResourceCallLists) + ":" + string(workspace.ActionRead)
	manage := string(workspace.ResourceCallLists) + ":" + string(workspace.ActionManage)
	leads := string(workspace.ResourceLeads) + ":" + string(workspace.ActionRead)
	cases := []struct {
		name        string
		permissions permissionSet
		want        Viewer
	}{
		{"no call list permission", permissionSet{}, Viewer{UserID: "u-1"}},
		{"a member of lists", permissionSet{read: true}, Viewer{UserID: "u-1", Views: true}},
		{"a list manager", permissionSet{read: true, manage: true, leads: true}, Viewer{UserID: "u-1", Views: true, Manages: true}},
		{"manage without its prerequisites", permissionSet{manage: true}, Viewer{UserID: "u-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ViewerOf(tc.permissions, "ws-1", "u-1", false); got != tc.want {
				t.Fatalf("ViewerOf = %+v, want %+v", got, tc.want)
			}
		})
	}
	if got := ViewerOf(nil, "ws-1", "u-1", true); got != (Viewer{UserID: "u-1"}) {
		t.Fatalf("a missing permission checker must refuse: %+v", got)
	}
}
