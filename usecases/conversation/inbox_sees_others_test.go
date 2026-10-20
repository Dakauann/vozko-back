package conversation_usecase

import (
	"testing"

	"vozko/domain/conversation"
)

type seesOthersAuthorizer struct {
	inboxServiceTestAuthorizer
	permissionAsked []string
}

func (a *seesOthersAuthorizer) HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool {
	a.permissionAsked = append(a.permissionAsked, resource+":"+action)
	return a.viewOthers
}

func TestTheInboxShowsOthersConversationsOnlyToWhoMaySeeThem(t *testing.T) {
	cases := []struct {
		name         string
		admin        bool
		ownerOrAdmin bool
		viewOthers   bool
		workspaceID  string
		want         string
	}{
		{"a member without view_others sees only their own", false, false, false, "ws-1", "user-1"},
		{"a member with view_others sees everyone's", false, false, true, "ws-1", ""},
		{"a workspace owner or admin sees everyone's", false, true, false, "ws-1", ""},
		{"a system admin sees everyone's", true, false, false, "ws-1", ""},
		{"without a workspace nothing widens the view", false, true, true, "", "user-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			history := &inboxServiceTestHistoryProvider{}
			authorizer := &seesOthersAuthorizer{inboxServiceTestAuthorizer: inboxServiceTestAuthorizer{
				canAccessCampaign: true, departmentScopeOkay: true, ownerOrAdmin: tc.ownerOrAdmin, viewOthers: tc.viewOthers,
			}}
			service := NewInboxService(history, nil, nil, &inboxServiceTestResolver{}, nil, authorizer, nil, nil)

			if _, _, err := service.SearchInbox("user-1", conversation.SearchInboxInput{
				WorkspaceID: tc.workspaceID, AssignedUserID: "user-1", IsAdmin: tc.admin, Page: 1, PageSize: 20,
			}); err != nil {
				t.Fatal(err)
			}

			if history.lastSearch.AssignedUserID != tc.want {
				t.Fatalf("assigned filter = %q, want %q", history.lastSearch.AssignedUserID, tc.want)
			}
			for _, asked := range authorizer.permissionAsked {
				if asked != "conversations:view_others" {
					t.Fatalf("asked %q, the rule is the conversations view_others permission", asked)
				}
			}
		})
	}
}
