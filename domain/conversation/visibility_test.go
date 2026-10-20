package conversation

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/shared"
)

type visibilityAuthorizer struct {
	scope         DepartmentAccessScope
	scopeAllowed  bool
	ownerOrAdmin  bool
	viewOthers    bool
	entryVisible  bool
	checkedEntry  string
	checkedAsUser string
}

func (a *visibilityAuthorizer) CanAccessEntry(userID, _, entryID, _ string, _ bool) bool {
	a.checkedEntry, a.checkedAsUser = entryID, userID
	return a.entryVisible
}

func (a *visibilityAuthorizer) CanAccessCampaign(string, string, string, string, bool) bool {
	return false
}

func (a *visibilityAuthorizer) GetAccessibleEntryIDs(string, string, bool) []string { return nil }

func (a *visibilityAuthorizer) GetDepartmentScope(_, _ string, isAdmin bool) (DepartmentAccessScope, bool) {
	if isAdmin {
		return DepartmentAccessScope{}, true
	}
	return a.scope, a.scopeAllowed
}

func (a *visibilityAuthorizer) HasWorkspacePermission(_, _, resource, action string, _ bool) bool {
	return resource == "conversations" && action == "view_others" && a.viewOthers
}

func (a *visibilityAuthorizer) IsWorkspaceMember(string, string) bool { return true }

func (a *visibilityAuthorizer) IsWorkspaceOwnerOrAdmin(string, string) bool { return a.ownerOrAdmin }

func TestVisibilityScopeIsTheInboxListRuleForTheViewer(t *testing.T) {
	member := shared.Person{UserID: "user-1"}
	cases := []struct {
		name       string
		viewer     shared.Person
		authorizer visibilityAuthorizer
		want       VisibilityScope
	}{
		{
			name:       "a system admin sees every conversation",
			viewer:     shared.Person{UserID: "root", SystemAdmin: true},
			authorizer: visibilityAuthorizer{scopeAllowed: false},
			want:       VisibilityScope{},
		},
		{
			name:       "a workspace owner sees every conversation",
			viewer:     member,
			authorizer: visibilityAuthorizer{scopeAllowed: true, ownerOrAdmin: true},
			want:       VisibilityScope{},
		},
		{
			name:       "a member with view_others and no departments sees every conversation",
			viewer:     member,
			authorizer: visibilityAuthorizer{scopeAllowed: true, viewOthers: true},
			want:       VisibilityScope{},
		},
		{
			name:   "a department member sees their departments and what is assigned to them",
			viewer: member,
			authorizer: visibilityAuthorizer{
				scopeAllowed: true, viewOthers: true,
				scope: DepartmentAccessScope{DepartmentIDs: []string{"dept-1"}, Restrict: true, WorkspaceHasDepartments: true},
			},
			want: VisibilityScope{ReadScope: ReadScope{DepartmentIDs: []string{"dept-1"}, RestrictDepartments: true, AssigneeOverrideUserID: "user-1"}},
		},
		{
			name:       "a member without view_others sees only unassigned conversations and their own",
			viewer:     member,
			authorizer: visibilityAuthorizer{scopeAllowed: true},
			want:       VisibilityScope{AssignedUserID: "user-1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authorizer := tc.authorizer
			got, err := NewVisibility(&authorizer).Scope(tc.viewer, "ws-1")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("scope = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestAViewerWhoseScopeCannotBeReadHasNoScope(t *testing.T) {
	cases := []struct {
		name       string
		viewer     shared.Person
		workspace  string
		authorizer *visibilityAuthorizer
	}{
		{"the department scope is unreadable", shared.Person{UserID: "user-1"}, "ws-1", &visibilityAuthorizer{scopeAllowed: false}},
		{"no viewer", shared.Person{}, "ws-1", &visibilityAuthorizer{scopeAllowed: true}},
		{"no workspace", shared.Person{UserID: "user-1"}, "", &visibilityAuthorizer{scopeAllowed: true}},
		{"no authorizer", shared.Person{UserID: "user-1"}, "ws-1", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var visibility Visibility
			if tc.authorizer == nil {
				visibility = NewVisibility(nil)
			} else {
				visibility = NewVisibility(tc.authorizer)
			}
			if _, err := visibility.Scope(tc.viewer, tc.workspace); !errors.Is(err, ErrNoConversationScope) {
				t.Fatalf("err = %v, want ErrNoConversationScope", err)
			}
		})
	}
}

func TestNothingVisibleHidesEveryConversation(t *testing.T) {
	scope := NothingVisible()
	if !scope.RestrictDepartments || len(scope.DepartmentIDs) != 0 || scope.AssigneeOverrideUserID != "" || scope.Unrestricted() {
		t.Fatalf("nothing visible must restrict to no department and no assignee: %+v", scope)
	}
}

func TestOnlyAScopeWithoutDepartmentOrOwnershipLimitsIsUnrestricted(t *testing.T) {
	cases := []struct {
		name  string
		scope VisibilityScope
		want  bool
	}{
		{"no limits", VisibilityScope{}, true},
		{"department limit", VisibilityScope{ReadScope: ReadScope{RestrictDepartments: true}}, false},
		{"ownership limit", VisibilityScope{AssignedUserID: "user-1"}, false},
	}
	for _, tc := range cases {
		if got := tc.scope.Unrestricted(); got != tc.want {
			t.Fatalf("%s: Unrestricted = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCanViewIsTheInboxEntryRule(t *testing.T) {
	cases := []struct {
		name      string
		viewer    shared.Person
		workspace string
		entryType string
		visible   bool
		want      bool
	}{
		{"the inbox rule grants the conversation", shared.Person{UserID: "user-1"}, "ws-1", "whatsapp", true, true},
		{"the inbox rule hides the conversation", shared.Person{UserID: "user-1"}, "ws-1", "whatsapp", false, false},
		{"a call is not a conversation", shared.Person{UserID: "user-1"}, "ws-1", "sip_call", true, false},
		{"no workspace", shared.Person{UserID: "user-1"}, "", "whatsapp", true, false},
		{"no viewer", shared.Person{}, "ws-1", "whatsapp", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authorizer := &visibilityAuthorizer{entryVisible: tc.visible}
			got, err := NewVisibility(authorizer).CanView(tc.viewer, tc.workspace, "entry-1", tc.entryType)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("CanView = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCanViewWithoutAnAuthorizerIsAnError(t *testing.T) {
	visible, err := NewVisibility(nil).CanView(shared.Person{UserID: "user-1"}, "ws-1", "entry-1", "whatsapp")
	if visible || !errors.Is(err, ErrNoConversationScope) {
		t.Fatalf("visible = %v, err = %v", visible, err)
	}
}
