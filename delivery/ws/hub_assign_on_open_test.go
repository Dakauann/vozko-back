package ws

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	wsc "vozko/domain/workspace_config"
)

type assignOnOpenAuthorizer struct {
	workspaceMembers map[string]bool
	ownerAdmins      map[string]bool
	roulettePerms    map[string]bool
}

func (a *assignOnOpenAuthorizer) CanAccessEntry(string, string, string, string, bool) bool {
	return true
}
func (a *assignOnOpenAuthorizer) CanAccessCampaign(string, string, string, string, bool) bool {
	return true
}
func (a *assignOnOpenAuthorizer) GetAccessibleEntryIDs(string, string, bool) []string { return nil }
func (a *assignOnOpenAuthorizer) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, true
}
func (a *assignOnOpenAuthorizer) HasWorkspacePermission(userID, _ string, _, action string, _ bool) bool {
	if action == "roulette" {
		if a.roulettePerms == nil {
			return true
		}
		return a.roulettePerms[userID]
	}
	return true
}
func (a *assignOnOpenAuthorizer) IsWorkspaceMember(userID, _ string) bool {
	if a.workspaceMembers == nil {
		return true
	}
	return a.workspaceMembers[userID]
}
func (a *assignOnOpenAuthorizer) IsWorkspaceOwnerOrAdmin(userID, _ string) bool {
	return a.ownerAdmins[userID]
}

type assignOnOpenResolver struct {
	workspaceID string
}

func (r *assignOnOpenResolver) GetCampaignWorkspaceID(string, string) (string, error) {
	return r.workspaceID, nil
}
func (r *assignOnOpenResolver) GetCampaignDepartmentID(string, string) (string, error) {
	return "", nil
}
func (r *assignOnOpenResolver) GetEntryWorkspaceID(string, string) (string, error) {
	return r.workspaceID, nil
}
func (r *assignOnOpenResolver) GetEntryDepartmentID(string, string) (string, error) { return "", nil }
func (r *assignOnOpenResolver) GetEntryCampaignID(string, string) (string, error)   { return "", nil }

type assignOnOpenRepo struct {
	assignments map[string]*ia.InboxAssignment
	assignCalls int
}

func newAssignOnOpenRepo() *assignOnOpenRepo {
	return &assignOnOpenRepo{assignments: make(map[string]*ia.InboxAssignment)}
}

func (r *assignOnOpenRepo) key(entryID, entryType string) string {
	return entryID + "|" + entryType
}

func (r *assignOnOpenRepo) FindByEntry(_, entryID, entryType string) (*ia.InboxAssignment, error) {
	return r.assignments[r.key(entryID, entryType)], nil
}
func (r *assignOnOpenRepo) FindByEntries(string, []string) ([]*ia.InboxAssignment, error) {
	return nil, nil
}
func (r *assignOnOpenRepo) FindByEntryAndUser(string, string, string, string) (*ia.InboxAssignment, error) {
	return nil, nil
}
func (r *assignOnOpenRepo) Assign(a *ia.InboxAssignment) error {
	r.assignCalls++
	r.assignments[r.key(a.EntryID, a.EntryType)] = a
	return nil
}
func (r *assignOnOpenRepo) Unassign(string, string, string) error { return nil }
func (r *assignOnOpenRepo) ListByUser(string, string, string) ([]string, error) {
	return nil, nil
}
func (r *assignOnOpenRepo) IsAssignedToUser(string, string, string, string) (bool, error) {
	return false, nil
}
func (r *assignOnOpenRepo) GetRoundRobinState(string, string, string) (*ia.RoundRobinState, error) {
	return nil, nil
}
func (r *assignOnOpenRepo) SaveRoundRobinState(*ia.RoundRobinState) error { return nil }

type assignOnOpenConfigRepo struct {
	skipAdmins bool
}

func (c *assignOnOpenConfigRepo) GetByWorkspaceID(context.Context, string) (*wsc.WorkspaceConfig, error) {
	return &wsc.WorkspaceConfig{SkipAdminAssignment: c.skipAdmins}, nil
}
func (c *assignOnOpenConfigRepo) Upsert(context.Context, *wsc.WorkspaceConfig) error { return nil }
func (c *assignOnOpenConfigRepo) EnsureExists(context.Context, string) error         { return nil }

// Entitlements have no bearing on who a conversation opens for.
func (c *assignOnOpenConfigRepo) GetIncludedUnofficialInstancesByWorkspaceIDs(
	context.Context, []string,
) (map[string]int, error) {
	return nil, nil
}

func buildHubForAssignOnOpen(auth *assignOnOpenAuthorizer, repo *assignOnOpenRepo, resolver *assignOnOpenResolver, cfgRepo *assignOnOpenConfigRepo) *ConversationHub {
	hub := NewConversationHub(auth, nil, nil, "test-replica", "")
	hub.SetAssignmentRepo(repo)
	hub.SetCampaignWorkspaceResolver(resolver)
	if cfgRepo != nil {
		hub.SetWorkspaceConfigRepo(cfgRepo)
	}
	return hub
}

func makeConn(userID, workspaceID string, isAdmin bool) *WSConnection {
	return &WSConnection{
		ID:          "conn-" + userID,
		UserID:      userID,
		WorkspaceID: workspaceID,
		IsAdmin:     isAdmin,
		Send:        make(chan []byte, 16),
	}
}

func TestTryAssignOnOpen_AssignsUnassignedEntry(t *testing.T) {
	repo := newAssignOnOpenRepo()
	auth := &assignOnOpenAuthorizer{}
	hub := buildHubForAssignOnOpen(auth, repo, &assignOnOpenResolver{workspaceID: "ws-1"}, nil)
	conn := makeConn("user-1", "ws-1", false)

	hub.tryAssignOnOpen(conn, "entry-1", "whatsapp")

	a := repo.assignments["entry-1|whatsapp"]
	require.NotNil(t, a, "entry should be assigned")
	assert.Equal(t, "user-1", a.AssignedUserID)
	assert.Equal(t, "ws-1", a.WorkspaceID)
	assert.Equal(t, "entry-1", a.EntryID)
	assert.Equal(t, "whatsapp", a.EntryType)
}

func TestTryAssignOnOpen_SkipsAlreadyAssigned(t *testing.T) {
	repo := newAssignOnOpenRepo()
	repo.assignments["entry-1|whatsapp"] = &ia.InboxAssignment{
		EntryID:        "entry-1",
		EntryType:      "whatsapp",
		AssignedUserID: "other-user",
	}
	auth := &assignOnOpenAuthorizer{}
	hub := buildHubForAssignOnOpen(auth, repo, &assignOnOpenResolver{workspaceID: "ws-1"}, nil)
	conn := makeConn("user-1", "ws-1", false)

	hub.tryAssignOnOpen(conn, "entry-1", "whatsapp")

	assert.Equal(t, "other-user", repo.assignments["entry-1|whatsapp"].AssignedUserID)
	assert.Equal(t, 0, repo.assignCalls, "Assign should not have been called")
}

func TestTryAssignOnOpen_SkipsSystemAdminNotWorkspaceMember(t *testing.T) {
	repo := newAssignOnOpenRepo()
	auth := &assignOnOpenAuthorizer{
		workspaceMembers: map[string]bool{"system-admin": false},
	}
	hub := buildHubForAssignOnOpen(auth, repo, &assignOnOpenResolver{workspaceID: "ws-1"}, nil)
	conn := makeConn("system-admin", "ws-1", true)

	hub.tryAssignOnOpen(conn, "entry-1", "whatsapp")

	assert.Nil(t, repo.assignments["entry-1|whatsapp"], "system admin non-member should not be assigned")
}

func TestTryAssignOnOpen_AllowsSystemAdminWhoIsWorkspaceMember(t *testing.T) {
	repo := newAssignOnOpenRepo()
	auth := &assignOnOpenAuthorizer{
		workspaceMembers: map[string]bool{"admin-member": true},
	}
	hub := buildHubForAssignOnOpen(auth, repo, &assignOnOpenResolver{workspaceID: "ws-1"}, nil)
	conn := makeConn("admin-member", "ws-1", true)

	hub.tryAssignOnOpen(conn, "entry-1", "whatsapp")

	a := repo.assignments["entry-1|whatsapp"]
	require.NotNil(t, a)
	assert.Equal(t, "admin-member", a.AssignedUserID)
}

func TestTryAssignOnOpen_SkipsOwnerWhenSkipAdminEnabled(t *testing.T) {
	repo := newAssignOnOpenRepo()
	auth := &assignOnOpenAuthorizer{
		ownerAdmins: map[string]bool{"owner": true},
	}
	cfgRepo := &assignOnOpenConfigRepo{skipAdmins: true}
	hub := buildHubForAssignOnOpen(auth, repo, &assignOnOpenResolver{workspaceID: "ws-1"}, cfgRepo)
	conn := makeConn("owner", "ws-1", false)

	hub.tryAssignOnOpen(conn, "entry-1", "whatsapp")

	assert.Nil(t, repo.assignments["entry-1|whatsapp"], "owner should not be assigned when SkipAdminAssignment is on")
}

func TestTryAssignOnOpen_AllowsOwnerWhenSkipAdminDisabled(t *testing.T) {
	repo := newAssignOnOpenRepo()
	auth := &assignOnOpenAuthorizer{
		ownerAdmins: map[string]bool{"owner": true},
	}
	cfgRepo := &assignOnOpenConfigRepo{skipAdmins: false}
	hub := buildHubForAssignOnOpen(auth, repo, &assignOnOpenResolver{workspaceID: "ws-1"}, cfgRepo)
	conn := makeConn("owner", "ws-1", false)

	hub.tryAssignOnOpen(conn, "entry-1", "whatsapp")

	a := repo.assignments["entry-1|whatsapp"]
	require.NotNil(t, a)
	assert.Equal(t, "owner", a.AssignedUserID)
}

func TestTryAssignOnOpen_SkipsUserWithoutRoulettePermission(t *testing.T) {
	repo := newAssignOnOpenRepo()
	auth := &assignOnOpenAuthorizer{
		roulettePerms: map[string]bool{"user-1": false},
	}
	hub := buildHubForAssignOnOpen(auth, repo, &assignOnOpenResolver{workspaceID: "ws-1"}, nil)
	conn := makeConn("user-1", "ws-1", false)

	hub.tryAssignOnOpen(conn, "entry-1", "whatsapp")

	assert.Nil(t, repo.assignments["entry-1|whatsapp"], "user without roulette permission should not be assigned")
}

func TestTryAssignOnOpen_AllowsUserWithRoulettePermission(t *testing.T) {
	repo := newAssignOnOpenRepo()
	auth := &assignOnOpenAuthorizer{
		roulettePerms: map[string]bool{"user-1": true},
	}
	hub := buildHubForAssignOnOpen(auth, repo, &assignOnOpenResolver{workspaceID: "ws-1"}, nil)
	conn := makeConn("user-1", "ws-1", false)

	hub.tryAssignOnOpen(conn, "entry-1", "whatsapp")

	a := repo.assignments["entry-1|whatsapp"]
	require.NotNil(t, a)
	assert.Equal(t, "user-1", a.AssignedUserID)
}

func TestTryAssignOnOpen_NoOpWhenNoAssignmentRepo(t *testing.T) {
	auth := &assignOnOpenAuthorizer{}
	hub := NewConversationHub(auth, nil, nil, "test-replica", "")

	conn := makeConn("user-1", "ws-1", false)

	hub.tryAssignOnOpen(conn, "entry-1", "whatsapp")
}

func TestTryAssignOnOpen_NoOpWhenNoWorkspaceResolver(t *testing.T) {
	repo := newAssignOnOpenRepo()
	auth := &assignOnOpenAuthorizer{}
	hub := NewConversationHub(auth, nil, nil, "test-replica", "")
	hub.SetAssignmentRepo(repo)

	conn := makeConn("user-1", "ws-1", false)

	hub.tryAssignOnOpen(conn, "entry-1", "whatsapp")

	assert.Empty(t, repo.assignments, "should not assign without workspace resolver")
}

func TestTryAssignOnOpen_MultipleUsersOpenSameEntry_OnlyFirstAssigns(t *testing.T) {
	repo := newAssignOnOpenRepo()
	auth := &assignOnOpenAuthorizer{}
	hub := buildHubForAssignOnOpen(auth, repo, &assignOnOpenResolver{workspaceID: "ws-1"}, nil)

	conn1 := makeConn("user-1", "ws-1", false)
	conn2 := makeConn("user-2", "ws-1", false)

	hub.tryAssignOnOpen(conn1, "entry-1", "whatsapp")
	a := repo.assignments["entry-1|whatsapp"]
	require.NotNil(t, a)
	assert.Equal(t, "user-1", a.AssignedUserID)

	hub.tryAssignOnOpen(conn2, "entry-1", "whatsapp")
	a = repo.assignments["entry-1|whatsapp"]
	assert.Equal(t, "user-1", a.AssignedUserID, "should not overwrite first assignment")
	assert.Equal(t, 1, repo.assignCalls, "Assign should only be called once")
}

func TestTryAssignOnOpen_DifferentEntriesAssignIndependently(t *testing.T) {
	repo := newAssignOnOpenRepo()
	auth := &assignOnOpenAuthorizer{}
	hub := buildHubForAssignOnOpen(auth, repo, &assignOnOpenResolver{workspaceID: "ws-1"}, nil)

	conn1 := makeConn("user-1", "ws-1", false)
	conn2 := makeConn("user-2", "ws-1", false)

	hub.tryAssignOnOpen(conn1, "entry-1", "whatsapp")
	hub.tryAssignOnOpen(conn2, "entry-2", "whatsapp")

	assert.Equal(t, "user-1", repo.assignments["entry-1|whatsapp"].AssignedUserID)
	assert.Equal(t, "user-2", repo.assignments["entry-2|whatsapp"].AssignedUserID)
}

func TestTryAssignOnOpen_VoiceEntryType(t *testing.T) {
	repo := newAssignOnOpenRepo()
	auth := &assignOnOpenAuthorizer{}
	hub := buildHubForAssignOnOpen(auth, repo, &assignOnOpenResolver{workspaceID: "ws-1"}, nil)
	conn := makeConn("user-1", "ws-1", false)

	hub.tryAssignOnOpen(conn, "entry-1", "voice")

	a := repo.assignments["entry-1|voice"]
	require.NotNil(t, a)
	assert.Equal(t, "user-1", a.AssignedUserID)
	assert.Equal(t, "voice", a.EntryType)
}

func TestTryAssignOnOpen_CombinedEdgeCase_AdminMemberWithSkipDisabledAndRoulette(t *testing.T) {

	repo := newAssignOnOpenRepo()
	auth := &assignOnOpenAuthorizer{
		workspaceMembers: map[string]bool{"admin-user": true},
		ownerAdmins:      map[string]bool{"admin-user": true},
		roulettePerms:    map[string]bool{"admin-user": true},
	}
	cfgRepo := &assignOnOpenConfigRepo{skipAdmins: false}
	hub := buildHubForAssignOnOpen(auth, repo, &assignOnOpenResolver{workspaceID: "ws-1"}, cfgRepo)
	conn := makeConn("admin-user", "ws-1", true)

	hub.tryAssignOnOpen(conn, "entry-1", "whatsapp")

	a := repo.assignments["entry-1|whatsapp"]
	require.NotNil(t, a)
	assert.Equal(t, "admin-user", a.AssignedUserID)
}

func TestTryAssignOnOpen_CombinedEdgeCase_AdminMemberWithSkipEnabled(t *testing.T) {

	repo := newAssignOnOpenRepo()
	auth := &assignOnOpenAuthorizer{
		ownerAdmins:   map[string]bool{"admin-user": true},
		roulettePerms: map[string]bool{"admin-user": true},
	}
	cfgRepo := &assignOnOpenConfigRepo{skipAdmins: true}
	hub := buildHubForAssignOnOpen(auth, repo, &assignOnOpenResolver{workspaceID: "ws-1"}, cfgRepo)
	conn := makeConn("admin-user", "ws-1", false)

	hub.tryAssignOnOpen(conn, "entry-1", "whatsapp")

	assert.Nil(t, repo.assignments["entry-1|whatsapp"])
}
