package ws

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
)

type hubDepartmentTestAuthorizer struct {
	entryAccess      map[string]bool
	viewOthers       map[string]bool
	campaignAccess   map[string]bool
	departmentScopes map[string]conversation.DepartmentAccessScope
	allowedUsers     map[string]bool
	ownerAdmins      map[string]bool
	workspaceMembers map[string]bool
}

func (a *hubDepartmentTestAuthorizer) CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool {
	if isAdmin {
		return true
	}
	return a.entryAccess[userID]
}

func (a *hubDepartmentTestAuthorizer) CanAccessCampaign(userID, workspaceID, campaignID, campaignType string, isAdmin bool) bool {
	if isAdmin || a.ownerAdmins[userID] {
		return true
	}
	key := userID + "|" + workspaceID + "|" + campaignType + "|" + campaignID
	allowed, ok := a.campaignAccess[key]
	if !ok {
		return false
	}
	return allowed
}

func (a *hubDepartmentTestAuthorizer) GetAccessibleEntryIDs(string, string, bool) []string {
	return nil
}

func (a *hubDepartmentTestAuthorizer) GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool) {
	if isAdmin || a.ownerAdmins[userID] {
		return conversation.DepartmentAccessScope{}, true
	}
	if a.allowedUsers != nil {
		if allowed, ok := a.allowedUsers[userID]; ok && !allowed {
			return conversation.DepartmentAccessScope{}, false
		}
	}
	if scope, ok := a.departmentScopes[userID]; ok {
		return scope, true
	}
	return conversation.DepartmentAccessScope{}, true
}

func (a *hubDepartmentTestAuthorizer) HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool {
	if action == "view_others" {
		return isSystemAdmin || a.viewOthers[userID]
	}
	return true
}

func (a *hubDepartmentTestAuthorizer) IsWorkspaceMember(userID, workspaceID string) bool {
	if a.workspaceMembers == nil {
		return true
	}
	return a.workspaceMembers[userID]
}

func (a *hubDepartmentTestAuthorizer) IsWorkspaceOwnerOrAdmin(userID, workspaceID string) bool {
	return a.ownerAdmins[userID]
}

type connectedUsersEvent struct {
	Type    WSEventType `json:"type"`
	Payload struct {
		Users []struct {
			UserID string `json:"user_id"`
		} `json:"users"`
	} `json:"payload"`
}

func drainConnectedUsers(t *testing.T, conn *WSConnection) []string {
	t.Helper()
	select {
	case raw := <-conn.Send:
		var event connectedUsersEvent
		require.NoError(t, json.Unmarshal(raw, &event))
		require.Equal(t, WSEventConnectedUsers, event.Type)
		ids := make([]string, 0, len(event.Payload.Users))
		for _, user := range event.Payload.Users {
			ids = append(ids, user.UserID)
		}
		return ids
	default:
		t.Fatal("expected connected users payload")
		return nil
	}
}

func TestBroadcastEntryRemovedLocal_FiltersUnauthorizedAndViewOthersConnections(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{
		entryAccess: map[string]bool{
			"authorized-user":  true,
			"view-others-user": true,
			"blocked-user":     false,
		},
		viewOthers: map[string]bool{
			"view-others-user": true,
		},
	}

	hub := NewConversationHub(authorizer, nil, nil, nil, "test-replica", "")
	authorizedConn := &WSConnection{ID: "conn-1", UserID: "authorized-user", WorkspaceID: "ws-1", Send: make(chan []byte, 1)}
	viewOthersConn := &WSConnection{ID: "conn-2", UserID: "view-others-user", WorkspaceID: "ws-1", Send: make(chan []byte, 1)}
	blockedConn := &WSConnection{ID: "conn-3", UserID: "blocked-user", WorkspaceID: "ws-1", Send: make(chan []byte, 1)}

	hub.connections[authorizedConn.ID] = authorizedConn
	hub.connections[viewOthersConn.ID] = viewOthersConn
	hub.connections[blockedConn.ID] = blockedConn
	hub.userConnections[authorizedConn.UserID] = map[string]bool{authorizedConn.ID: true}
	hub.userConnections[viewOthersConn.UserID] = map[string]bool{viewOthersConn.ID: true}
	hub.userConnections[blockedConn.UserID] = map[string]bool{blockedConn.ID: true}

	hub.broadcastEntryRemovedLocal("entry-1", "whatsapp", "ws-1", "assigned-user")

	require.Len(t, authorizedConn.Send, 1)
	require.Len(t, viewOthersConn.Send, 0)
	require.Len(t, blockedConn.Send, 0)
}

func TestBroadcastConnectedUsersToWorkspace_GlobalMemberSeesOnlyRelevantDepartmentScope(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{
		campaignAccess: map[string]bool{
			"viewer|ws-1|whatsapp|camp-1": true,
			"viewer|ws-1|whatsapp|camp-2": false,
		},
		departmentScopes: map[string]conversation.DepartmentAccessScope{
			"viewer":        {DepartmentIDs: []string{"dept-1"}, Restrict: true},
			"same-dept":     {DepartmentIDs: []string{"dept-1"}, Restrict: true},
			"other-dept":    {DepartmentIDs: []string{"dept-2"}, Restrict: true},
			"campaign-user": {DepartmentIDs: []string{"dept-1"}, Restrict: true},
		},
		ownerAdmins: map[string]bool{
			"owner-user": true,
		},
		workspaceMembers: map[string]bool{
			"viewer":        true,
			"same-dept":     true,
			"other-dept":    true,
			"owner-user":    true,
			"campaign-user": true,
			"blocked-camp":  true,
		},
	}

	hub := NewConversationHub(authorizer, nil, nil, nil, "test-replica", "")
	viewerConn := &WSConnection{ID: "viewer", UserID: "viewer", WorkspaceID: "ws-1", DepartmentID: "dept-1", ViewMode: "global", Send: make(chan []byte, 1)}
	sameDeptConn := &WSConnection{ID: "same-dept", UserID: "same-dept", WorkspaceID: "ws-1", DepartmentID: "dept-1", ViewMode: "global", Send: make(chan []byte, 1)}
	otherDeptConn := &WSConnection{ID: "other-dept", UserID: "other-dept", WorkspaceID: "ws-1", DepartmentID: "dept-2", ViewMode: "global", Send: make(chan []byte, 1)}
	ownerConn := &WSConnection{ID: "owner-user", UserID: "owner-user", WorkspaceID: "ws-1", ViewMode: "global", Send: make(chan []byte, 1)}
	inScopeCampaignConn := &WSConnection{ID: "campaign-user", UserID: "campaign-user", WorkspaceID: "ws-1", CampaignID: "camp-1", CampaignType: "whatsapp", ViewMode: "campaign", Send: make(chan []byte, 1)}
	blockedCampaignConn := &WSConnection{ID: "blocked-camp", UserID: "blocked-camp", WorkspaceID: "ws-1", CampaignID: "camp-2", CampaignType: "whatsapp", ViewMode: "campaign", Send: make(chan []byte, 1)}

	hub.connections[viewerConn.ID] = viewerConn
	hub.connections[sameDeptConn.ID] = sameDeptConn
	hub.connections[otherDeptConn.ID] = otherDeptConn
	hub.connections[ownerConn.ID] = ownerConn
	hub.connections[inScopeCampaignConn.ID] = inScopeCampaignConn
	hub.connections[blockedCampaignConn.ID] = blockedCampaignConn

	hub.broadcastConnectedUsersToWorkspace("ws-1")

	ids := drainConnectedUsers(t, viewerConn)
	require.ElementsMatch(t, []string{"viewer", "same-dept", "owner-user", "campaign-user"}, ids)
}

func TestBroadcastConnectedUsersToWorkspace_CampaignViewShowsUsersWhoCanAccessCampaign(t *testing.T) {
	authorizer := &hubDepartmentTestAuthorizer{
		campaignAccess: map[string]bool{
			"viewer|ws-1|whatsapp|camp-1":               true,
			"same-campaign|ws-1|whatsapp|camp-1":        true,
			"global-same-scope|ws-1|whatsapp|camp-1":    true,
			"other-campaign-scope|ws-1|whatsapp|camp-1": true,
			"global-other-scope|ws-1|whatsapp|camp-1":   false,
		},
		departmentScopes: map[string]conversation.DepartmentAccessScope{
			"viewer":               {DepartmentIDs: []string{"dept-1"}, Restrict: true},
			"global-same-scope":    {DepartmentIDs: []string{"dept-1"}, Restrict: true},
			"other-campaign-scope": {DepartmentIDs: []string{"dept-1"}, Restrict: true},
			"global-other-scope":   {DepartmentIDs: []string{"dept-2"}, Restrict: true},
		},
		ownerAdmins: map[string]bool{
			"owner-user": true,
		},
		workspaceMembers: map[string]bool{
			"viewer":               true,
			"same-campaign":        true,
			"global-same-scope":    true,
			"other-campaign-scope": true,
			"global-other-scope":   true,
			"owner-user":           true,
		},
	}

	hub := NewConversationHub(authorizer, nil, nil, nil, "test-replica", "")
	viewerConn := &WSConnection{ID: "viewer", UserID: "viewer", WorkspaceID: "ws-1", CampaignID: "camp-1", CampaignType: "whatsapp", ViewMode: "campaign", Send: make(chan []byte, 1)}
	sameCampaignConn := &WSConnection{ID: "same-campaign", UserID: "same-campaign", WorkspaceID: "ws-1", CampaignID: "camp-1", CampaignType: "whatsapp", ViewMode: "campaign", Send: make(chan []byte, 1)}
	globalSameScopeConn := &WSConnection{ID: "global-same-scope", UserID: "global-same-scope", WorkspaceID: "ws-1", DepartmentID: "dept-1", ViewMode: "global", Send: make(chan []byte, 1)}
	otherCampaignScopeConn := &WSConnection{ID: "other-campaign-scope", UserID: "other-campaign-scope", WorkspaceID: "ws-1", CampaignID: "camp-2", CampaignType: "whatsapp", ViewMode: "campaign", Send: make(chan []byte, 1)}
	globalOtherScopeConn := &WSConnection{ID: "global-other-scope", UserID: "global-other-scope", WorkspaceID: "ws-1", DepartmentID: "dept-2", ViewMode: "global", Send: make(chan []byte, 1)}
	ownerConn := &WSConnection{ID: "owner-user", UserID: "owner-user", WorkspaceID: "ws-1", ViewMode: "global", Send: make(chan []byte, 1)}

	hub.connections[viewerConn.ID] = viewerConn
	hub.connections[sameCampaignConn.ID] = sameCampaignConn
	hub.connections[globalSameScopeConn.ID] = globalSameScopeConn
	hub.connections[otherCampaignScopeConn.ID] = otherCampaignScopeConn
	hub.connections[globalOtherScopeConn.ID] = globalOtherScopeConn
	hub.connections[ownerConn.ID] = ownerConn

	hub.broadcastConnectedUsersToWorkspace("ws-1")

	ids := drainConnectedUsers(t, viewerConn)
	require.ElementsMatch(t, []string{"viewer", "same-campaign", "global-same-scope", "other-campaign-scope", "owner-user"}, ids)
}
