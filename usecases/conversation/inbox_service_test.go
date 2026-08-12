package conversation_usecase

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type inboxServiceTestHistoryProvider struct {
	searchCalls int
	lastSearch  conversation.SearchInboxInput
	entries     []conversation.InboxEntry
	totalItems  int64
}

func (p *inboxServiceTestHistoryProvider) GetHistory(string, shared.EntryType, int) ([]*conversation.Message, bool, int64, error) {
	return nil, false, 0, nil
}

func (p *inboxServiceTestHistoryProvider) GetHistoryBefore(string, shared.EntryType, time.Time, int) ([]*conversation.Message, bool, error) {
	return nil, false, nil
}

func (p *inboxServiceTestHistoryProvider) GetHistoryAround(string, shared.EntryType, time.Time, int) ([]*conversation.Message, bool, bool, int64, error) {
	return nil, false, false, 0, nil
}

func (p *inboxServiceTestHistoryProvider) GetUnreadCount(string, shared.EntryType) (int64, error) {
	return 0, nil
}

func (p *inboxServiceTestHistoryProvider) GetEntryInfo(string, string) (string, string, string, map[string]interface{}, []string, bool, error) {
	return "", "", "", nil, nil, true, nil
}

func (p *inboxServiceTestHistoryProvider) ResolveSenderIdentity(string, string, *conversation.Message) {
}

func (p *inboxServiceTestHistoryProvider) GetWindowStatusForEntry(string, string) conversation.WindowState {
	return conversation.ClosedWindow(conversation.WindowReasonExpired)
}

func (p *inboxServiceTestHistoryProvider) GetInboxEntries(string, string, string, string, int, int) ([]conversation.InboxEntry, int64, error) {
	return nil, 0, nil
}

func (p *inboxServiceTestHistoryProvider) GetInboxEntry(string, string) (*conversation.InboxEntry, error) {
	return nil, nil
}

func (p *inboxServiceTestHistoryProvider) SearchInboxEntries(input conversation.SearchInboxInput) ([]conversation.InboxEntry, int64, error) {
	p.searchCalls++
	p.lastSearch = input
	return append([]conversation.InboxEntry(nil), p.entries...), p.totalItems, nil
}

type inboxServiceTestAuthorizer struct {
	canAccessCampaign   bool
	departmentScope     conversation.DepartmentAccessScope
	departmentScopeOkay bool
	ownerOrAdmin        bool
	viewOthers          bool
}

func (a *inboxServiceTestAuthorizer) CanAccessEntry(string, string, string, string, bool) bool {
	return true
}

func (a *inboxServiceTestAuthorizer) CanAccessCampaign(string, string, string, string, bool) bool {
	return a.canAccessCampaign
}

func (a *inboxServiceTestAuthorizer) GetAccessibleEntryIDs(string, string, bool) []string {
	return nil
}

func (a *inboxServiceTestAuthorizer) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return a.departmentScope, a.departmentScopeOkay
}

func (a *inboxServiceTestAuthorizer) HasWorkspacePermission(string, string, string, string, bool) bool {
	return a.viewOthers
}

func (a *inboxServiceTestAuthorizer) IsWorkspaceMember(string, string) bool {
	return true
}

func (a *inboxServiceTestAuthorizer) IsWorkspaceOwnerOrAdmin(string, string) bool {
	return a.ownerOrAdmin
}

type inboxServiceTestResolver struct {
	workspaceID string
}

func (r *inboxServiceTestResolver) GetCampaignWorkspaceID(string, string) (string, error) {
	return r.workspaceID, nil
}

func (r *inboxServiceTestResolver) GetCampaignDepartmentID(string, string) (string, error) {
	return "dept-1", nil
}

func (r *inboxServiceTestResolver) GetEntryWorkspaceID(string, string) (string, error) {
	return r.workspaceID, nil
}

func (r *inboxServiceTestResolver) GetEntryDepartmentID(string, string) (string, error) {
	return "dept-1", nil
}

func (r *inboxServiceTestResolver) GetEntryCampaignID(string, string) (string, error) {
	return "campaign-1", nil
}

func TestSearchInbox_AppliesDepartmentScopeToWorkspaceSearch(t *testing.T) {
	historyProvider := &inboxServiceTestHistoryProvider{}
	authorizer := &inboxServiceTestAuthorizer{
		canAccessCampaign:   true,
		departmentScope:     conversation.DepartmentAccessScope{DepartmentIDs: []string{"dept-1"}, Restrict: true},
		departmentScopeOkay: true,
	}
	service := NewInboxService(historyProvider, nil, nil, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, nil, nil)

	_, _, err := service.SearchInbox("user-1", conversation.SearchInboxInput{
		WorkspaceID:    "ws-1",
		AssignedUserID: "user-1",
		Page:           1,
		PageSize:       20,
		SortOrder:      "desc",
	})

	require.NoError(t, err)
	require.Equal(t, 1, historyProvider.searchCalls)
	require.True(t, historyProvider.lastSearch.RestrictDepartments)
	require.Equal(t, []string{"dept-1"}, historyProvider.lastSearch.DepartmentIDs)
	require.Equal(t, "user-1", historyProvider.lastSearch.AssignedUserID)
}

// The user-facing "filter by responsible" must SURVIVE the permission-clearing that
// wipes AssignedUserID for privileged users, otherwise an admin filtering by a
// responsible would silently see everyone instead of that person's conversations.
func TestSearchInbox_ResponsibleFilterSurvivesPermissionClear(t *testing.T) {
	historyProvider := &inboxServiceTestHistoryProvider{}
	authorizer := &inboxServiceTestAuthorizer{
		canAccessCampaign:   true,
		departmentScope:     conversation.DepartmentAccessScope{Restrict: false},
		departmentScopeOkay: true,
	}
	service := NewInboxService(historyProvider, nil, nil, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, nil, nil)

	_, _, err := service.SearchInbox("admin-1", conversation.SearchInboxInput{
		WorkspaceID:       "ws-1",
		IsAdmin:           true,      // triggers the AssignedUserID permission-clear
		AssignedUserID:    "admin-1", // permission scope, must be cleared
		ResponsibleUserID: "agent-9", // user-facing filter, must survive
		Page:              1,
		PageSize:          20,
		SortOrder:         "desc",
	})

	require.NoError(t, err)
	require.Equal(t, 1, historyProvider.searchCalls)
	require.Empty(t, historyProvider.lastSearch.AssignedUserID, "permission scope should be cleared for admin")
	require.Equal(t, "agent-9", historyProvider.lastSearch.ResponsibleUserID, "responsible filter must survive perm-clear")
}

func TestSearchInbox_DeniesCampaignOutsideDepartmentScope(t *testing.T) {
	historyProvider := &inboxServiceTestHistoryProvider{}
	authorizer := &inboxServiceTestAuthorizer{
		canAccessCampaign:   false,
		departmentScope:     conversation.DepartmentAccessScope{DepartmentIDs: []string{"dept-1"}, Restrict: true},
		departmentScopeOkay: true,
	}
	service := NewInboxService(historyProvider, nil, nil, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, nil, nil)

	_, _, err := service.SearchInbox("user-1", conversation.SearchInboxInput{
		CampaignID:     "campaign-1",
		CampaignType:   "whatsapp",
		AssignedUserID: "user-1",
		Page:           1,
		PageSize:       20,
	})

	require.ErrorContains(t, err, "unauthorized")
	require.Zero(t, historyProvider.searchCalls)
}

func TestSearchInbox_SelectedDepartmentNarrowsScopeForEmployee(t *testing.T) {
	historyProvider := &inboxServiceTestHistoryProvider{}
	authorizer := &inboxServiceTestAuthorizer{
		canAccessCampaign:   true,
		departmentScope:     conversation.DepartmentAccessScope{DepartmentIDs: []string{"dept-1", "dept-2"}, Restrict: true},
		departmentScopeOkay: true,
	}
	service := NewInboxService(historyProvider, nil, nil, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, nil, nil)

	_, _, err := service.SearchInbox("user-1", conversation.SearchInboxInput{
		WorkspaceID:          "ws-1",
		SelectedDepartmentID: "dept-2",
		AssignedUserID:       "user-1",
		Page:                 1,
		PageSize:             20,
	})

	require.NoError(t, err)
	require.Equal(t, 1, historyProvider.searchCalls)
	require.True(t, historyProvider.lastSearch.RestrictDepartments)
	require.Equal(t, []string{"dept-2"}, historyProvider.lastSearch.DepartmentIDs)
}

func TestSearchInbox_SelectedDepartmentDeniesWhenNotInUserDepartments(t *testing.T) {
	historyProvider := &inboxServiceTestHistoryProvider{}
	authorizer := &inboxServiceTestAuthorizer{
		canAccessCampaign:   true,
		departmentScope:     conversation.DepartmentAccessScope{DepartmentIDs: []string{"dept-1", "dept-2"}, Restrict: true},
		departmentScopeOkay: true,
	}
	service := NewInboxService(historyProvider, nil, nil, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, nil, nil)

	_, _, err := service.SearchInbox("user-1", conversation.SearchInboxInput{
		WorkspaceID:          "ws-1",
		SelectedDepartmentID: "dept-other",
		AssignedUserID:       "user-1",
		Page:                 1,
		PageSize:             20,
	})

	require.ErrorContains(t, err, "unauthorized")
	require.ErrorContains(t, err, "department")
	require.Zero(t, historyProvider.searchCalls)
}

func TestSearchInbox_SelectedDepartmentNarrowsScopeForAdmin(t *testing.T) {
	historyProvider := &inboxServiceTestHistoryProvider{}
	authorizer := &inboxServiceTestAuthorizer{
		canAccessCampaign:   true,
		departmentScope:     conversation.DepartmentAccessScope{WorkspaceHasDepartments: true},
		departmentScopeOkay: true,
		ownerOrAdmin:        true,
	}
	service := NewInboxService(historyProvider, nil, nil, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, nil, nil)

	_, _, err := service.SearchInbox("admin-1", conversation.SearchInboxInput{
		WorkspaceID:          "ws-1",
		SelectedDepartmentID: "dept-sales",
		AssignedUserID:       "admin-1",
		Page:                 1,
		PageSize:             20,
	})

	require.NoError(t, err)
	require.Equal(t, 1, historyProvider.searchCalls)
	require.True(t, historyProvider.lastSearch.RestrictDepartments)
	require.Equal(t, []string{"dept-sales"}, historyProvider.lastSearch.DepartmentIDs)

	require.Empty(t, historyProvider.lastSearch.AssignedUserID)
}

func TestSearchInbox_NoDepartmentSelectionShowsAllUserDepartments(t *testing.T) {
	historyProvider := &inboxServiceTestHistoryProvider{}
	authorizer := &inboxServiceTestAuthorizer{
		canAccessCampaign:   true,
		departmentScope:     conversation.DepartmentAccessScope{DepartmentIDs: []string{"dept-1", "dept-2", "dept-3"}, Restrict: true},
		departmentScopeOkay: true,
	}
	service := NewInboxService(historyProvider, nil, nil, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, nil, nil)

	_, _, err := service.SearchInbox("user-1", conversation.SearchInboxInput{
		WorkspaceID:    "ws-1",
		AssignedUserID: "user-1",
		Page:           1,
		PageSize:       20,
	})

	require.NoError(t, err)
	require.Equal(t, 1, historyProvider.searchCalls)
	require.True(t, historyProvider.lastSearch.RestrictDepartments)
	require.Equal(t, []string{"dept-1", "dept-2", "dept-3"}, historyProvider.lastSearch.DepartmentIDs)
}

func TestSearchInbox_AdminNoDepartmentSelectionSeesAll(t *testing.T) {
	historyProvider := &inboxServiceTestHistoryProvider{}
	authorizer := &inboxServiceTestAuthorizer{
		canAccessCampaign:   true,
		departmentScope:     conversation.DepartmentAccessScope{},
		departmentScopeOkay: true,
		ownerOrAdmin:        true,
	}
	service := NewInboxService(historyProvider, nil, nil, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, nil, nil)

	_, _, err := service.SearchInbox("admin-1", conversation.SearchInboxInput{
		WorkspaceID:    "ws-1",
		AssignedUserID: "admin-1",
		Page:           1,
		PageSize:       20,
	})

	require.NoError(t, err)
	require.Equal(t, 1, historyProvider.searchCalls)
	require.False(t, historyProvider.lastSearch.RestrictDepartments)
	require.Empty(t, historyProvider.lastSearch.DepartmentIDs)
}

func TestSearchInbox_DepartmentScopeDenied(t *testing.T) {
	historyProvider := &inboxServiceTestHistoryProvider{}
	authorizer := &inboxServiceTestAuthorizer{
		canAccessCampaign:   true,
		departmentScopeOkay: false,
	}
	service := NewInboxService(historyProvider, nil, nil, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, nil, nil)

	_, _, err := service.SearchInbox("user-1", conversation.SearchInboxInput{
		WorkspaceID:    "ws-1",
		AssignedUserID: "user-1",
		Page:           1,
		PageSize:       20,
	})

	require.ErrorContains(t, err, "unauthorized")
	require.Zero(t, historyProvider.searchCalls)
}

func TestSearchInbox_NoDeptWorkspaceEmployeeSeesAll(t *testing.T) {
	historyProvider := &inboxServiceTestHistoryProvider{}
	authorizer := &inboxServiceTestAuthorizer{
		canAccessCampaign:   true,
		departmentScope:     conversation.DepartmentAccessScope{},
		departmentScopeOkay: true,
	}
	service := NewInboxService(historyProvider, nil, nil, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, nil, nil)

	_, _, err := service.SearchInbox("user-1", conversation.SearchInboxInput{
		WorkspaceID:    "ws-1",
		AssignedUserID: "user-1",
		Page:           1,
		PageSize:       20,
	})

	require.NoError(t, err)
	require.Equal(t, 1, historyProvider.searchCalls)
	require.False(t, historyProvider.lastSearch.RestrictDepartments)
	require.Empty(t, historyProvider.lastSearch.DepartmentIDs)
}

func TestSearchInbox_NoDeptWorkspaceIgnoresStaleDepartmentID(t *testing.T) {
	historyProvider := &inboxServiceTestHistoryProvider{}
	authorizer := &inboxServiceTestAuthorizer{
		canAccessCampaign:   true,
		departmentScope:     conversation.DepartmentAccessScope{},
		departmentScopeOkay: true,
	}
	service := NewInboxService(historyProvider, nil, nil, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, nil, nil)

	_, _, err := service.SearchInbox("user-1", conversation.SearchInboxInput{
		WorkspaceID:          "ws-1",
		SelectedDepartmentID: "dept-stale",
		AssignedUserID:       "user-1",
		Page:                 1,
		PageSize:             20,
	})

	require.NoError(t, err)
	require.Equal(t, 1, historyProvider.searchCalls)

	require.False(t, historyProvider.lastSearch.RestrictDepartments)
	require.Empty(t, historyProvider.lastSearch.DepartmentIDs)
}

func TestSearchInbox_NoDeptWorkspaceAdminIgnoresStaleDepartmentID(t *testing.T) {
	historyProvider := &inboxServiceTestHistoryProvider{}
	authorizer := &inboxServiceTestAuthorizer{
		canAccessCampaign:   true,
		departmentScope:     conversation.DepartmentAccessScope{},
		departmentScopeOkay: true,
		ownerOrAdmin:        true,
	}
	service := NewInboxService(historyProvider, nil, nil, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, nil, nil)

	_, _, err := service.SearchInbox("admin-1", conversation.SearchInboxInput{
		WorkspaceID:          "ws-1",
		SelectedDepartmentID: "dept-stale",
		AssignedUserID:       "admin-1",
		Page:                 1,
		PageSize:             20,
	})

	require.NoError(t, err)
	require.Equal(t, 1, historyProvider.searchCalls)
	require.False(t, historyProvider.lastSearch.RestrictDepartments)
	require.Empty(t, historyProvider.lastSearch.DepartmentIDs)
	require.Empty(t, historyProvider.lastSearch.AssignedUserID)
}

func TestSearchInbox_SystemAdminSeesAllNoRestriction(t *testing.T) {
	historyProvider := &inboxServiceTestHistoryProvider{}
	authorizer := &inboxServiceTestAuthorizer{
		canAccessCampaign:   true,
		departmentScope:     conversation.DepartmentAccessScope{},
		departmentScopeOkay: true,
	}
	service := NewInboxService(historyProvider, nil, nil, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, nil, nil)

	_, _, err := service.SearchInbox("sys-admin", conversation.SearchInboxInput{
		WorkspaceID:    "ws-1",
		AssignedUserID: "sys-admin",
		IsAdmin:        true,
		Page:           1,
		PageSize:       20,
	})

	require.NoError(t, err)
	require.Equal(t, 1, historyProvider.searchCalls)
	require.False(t, historyProvider.lastSearch.RestrictDepartments)
	require.Empty(t, historyProvider.lastSearch.DepartmentIDs)

	require.Empty(t, historyProvider.lastSearch.AssignedUserID)
}

func TestSearchInbox_EmployeeOneDeptAutoScopesWithoutSelection(t *testing.T) {
	historyProvider := &inboxServiceTestHistoryProvider{}
	authorizer := &inboxServiceTestAuthorizer{
		canAccessCampaign:   true,
		departmentScope:     conversation.DepartmentAccessScope{DepartmentIDs: []string{"dept-only"}, Restrict: true, WorkspaceHasDepartments: true},
		departmentScopeOkay: true,
	}
	service := NewInboxService(historyProvider, nil, nil, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, nil, nil)

	_, _, err := service.SearchInbox("user-1", conversation.SearchInboxInput{
		WorkspaceID:    "ws-1",
		AssignedUserID: "user-1",
		Page:           1,
		PageSize:       20,
	})

	require.NoError(t, err)
	require.Equal(t, 1, historyProvider.searchCalls)
	require.True(t, historyProvider.lastSearch.RestrictDepartments)
	require.Equal(t, []string{"dept-only"}, historyProvider.lastSearch.DepartmentIDs)
}
