package inbox_assignment_usecase

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vozko/domain/workspace"
	workspace_department "vozko/domain/workspace/workspace_department"
)

type stubMembers struct {
	members []*workspace.Member
	err     error
	calls   int
}

func (s *stubMembers) ListMembers(string) ([]*workspace.Member, error) {
	s.calls++
	return s.members, s.err
}

type stubDeptMembers struct {
	byDept map[string][]workspace_department.DepartmentMember
	err    error
}

func (s *stubDeptMembers) ListMembers(departmentID string) ([]workspace_department.DepartmentMember, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.byDept[departmentID], nil
}

type stubAuthz struct {
	roulette     map[string]bool
	ownerOrAdmin map[string]bool
}

func (a *stubAuthz) HasWorkspacePermission(userID, _, _, _ string, _ bool) bool {
	return a.roulette[userID]
}
func (a *stubAuthz) IsWorkspaceOwnerOrAdmin(userID, _ string) bool { return a.ownerOrAdmin[userID] }

type memShared struct {
	values map[string]string
	getErr error
	setErr error
	writes int
}

func newMemShared() *memShared { return &memShared{values: map[string]string{}} }

func (m *memShared) GetString(key string) (string, error) {
	if m.getErr != nil {
		return "", m.getErr
	}
	return m.values[key], nil
}
func (m *memShared) SetString(key, value string, _ time.Duration) error {
	m.writes++
	if m.setErr != nil {
		return m.setErr
	}
	m.values[key] = value
	return nil
}

func member(userID string, role workspace.Role) *workspace.Member {
	return &workspace.Member{ID: "m-" + userID, UserID: userID, Role: role}
}

func newRoster(t *testing.T, members *stubMembers, depts *stubDeptMembers, authz *stubAuthz, shared *memShared) *RosterService {
	t.Helper()
	var deptPort departmentMemberLister
	if depts != nil {
		deptPort = depts
	}
	var cachePort rosterCache
	if shared != nil {
		cachePort = shared
	}
	return NewRosterService(members, deptPort, authz, cachePort)
}

func TestRoster_KeepsOnlyMembersWithTheRoulettePermission(t *testing.T) {
	members := &stubMembers{members: []*workspace.Member{
		member("ana", workspace.RoleMember),
		member("bob", workspace.RoleMember),
	}}
	authz := &stubAuthz{roulette: map[string]bool{"ana": true}}

	got, err := newRoster(t, members, nil, authz, nil).ListRouletteMembers("ws-1", "", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"ana"}, got)
}

func TestRoster_SkipAdmins(t *testing.T) {
	members := &stubMembers{members: []*workspace.Member{
		member("owner", workspace.RoleOwner),
		member("ana", workspace.RoleMember),
	}}
	authz := &stubAuthz{
		roulette:     map[string]bool{"owner": true, "ana": true},
		ownerOrAdmin: map[string]bool{"owner": true},
	}

	with, err := newRoster(t, members, nil, authz, nil).ListRouletteMembers("ws-1", "", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"owner", "ana"}, with)

	members.members = []*workspace.Member{member("owner", workspace.RoleOwner), member("ana", workspace.RoleMember)}
	without, err := newRoster(t, members, nil, authz, nil).ListRouletteMembers("ws-1", "", true)
	require.NoError(t, err)
	assert.Equal(t, []string{"ana"}, without)
}

func TestRoster_DepartmentNarrowing(t *testing.T) {
	members := &stubMembers{members: []*workspace.Member{
		member("ana", workspace.RoleMember),
		member("bob", workspace.RoleMember),
		member("cid", workspace.RoleMember),
	}}
	depts := &stubDeptMembers{byDept: map[string][]workspace_department.DepartmentMember{
		"dept-a": {{UserID: "ana"}, {UserID: "bob"}},
		"dept-b": {{UserID: "bob"}, {UserID: "cid"}},
	}}
	authz := &stubAuthz{roulette: map[string]bool{"ana": true, "bob": true, "cid": true}}

	a, err := newRoster(t, members, depts, authz, nil).ListRouletteMembers("ws-1", "dept-a", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"ana", "bob"}, a)

	b, err := newRoster(t, members, depts, authz, nil).ListRouletteMembers("ws-1", "dept-b", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"bob", "cid"}, b, "a member of two departments belongs to both rings")
}

func TestRoster_EmptyDepartment(t *testing.T) {
	members := &stubMembers{members: []*workspace.Member{member("ana", workspace.RoleMember)}}
	depts := &stubDeptMembers{byDept: map[string][]workspace_department.DepartmentMember{}}
	authz := &stubAuthz{roulette: map[string]bool{"ana": true}}

	got, err := newRoster(t, members, depts, authz, nil).ListRouletteMembers("ws-1", "dept-gone", false)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestRoster_DepartmentRepoMissing(t *testing.T) {
	members := &stubMembers{members: []*workspace.Member{member("ana", workspace.RoleMember)}}
	authz := &stubAuthz{roulette: map[string]bool{"ana": true}}

	got, err := newRoster(t, members, nil, authz, nil).ListRouletteMembers("ws-1", "dept-a", false)
	require.NoError(t, err)
	assert.Empty(t, got, "a department-scoped ask with no department repository must not fall back to the whole workspace")
}

func TestRoster_PropagatesErrors(t *testing.T) {
	boom := errors.New("boom")
	authz := &stubAuthz{}

	_, err := newRoster(t, &stubMembers{err: boom}, nil, authz, nil).ListRouletteMembers("ws-1", "", false)
	assert.ErrorIs(t, err, boom)

	members := &stubMembers{members: []*workspace.Member{member("ana", workspace.RoleMember)}}
	_, err = newRoster(t, members, &stubDeptMembers{err: boom}, authz, nil).ListRouletteMembers("ws-1", "dept-a", false)
	assert.ErrorIs(t, err, boom)
}

func TestRoster_TruncatesLargeWorkspaces(t *testing.T) {
	all := make([]*workspace.Member, 0, MaxRosterSize+50)
	perms := map[string]bool{}
	for i := 0; i < MaxRosterSize+50; i++ {
		uid := fmt.Sprintf("u-%04d", i)
		all = append(all, member(uid, workspace.RoleMember))
		perms[uid] = true
	}

	got, err := newRoster(t, &stubMembers{members: all}, nil, &stubAuthz{roulette: perms}, nil).
		ListRouletteMembers("ws-1", "", false)
	require.NoError(t, err)
	assert.Len(t, got, MaxRosterSize)
}

func TestRoster_CacheHitSkipsTheScan(t *testing.T) {
	members := &stubMembers{members: []*workspace.Member{
		member("ana", workspace.RoleMember),
		member("bob", workspace.RoleMember),
	}}
	authz := &stubAuthz{roulette: map[string]bool{"ana": true, "bob": true}}
	shared := newMemShared()
	svc := newRoster(t, members, nil, authz, shared)

	first, err := svc.ListRouletteMembers("ws-1", "", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"ana", "bob"}, first)
	assert.Equal(t, 1, members.calls)

	second, err := svc.ListRouletteMembers("ws-1", "", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"ana", "bob"}, second)
	assert.Equal(t, 1, members.calls, "a cache hit must not re-scan the workspace")
}

func TestRoster_CachesTheEmptyResult(t *testing.T) {
	members := &stubMembers{members: []*workspace.Member{member("ana", workspace.RoleMember)}}
	shared := newMemShared()
	svc := newRoster(t, members, nil, &stubAuthz{}, shared)

	first, err := svc.ListRouletteMembers("ws-1", "", false)
	require.NoError(t, err)
	assert.Empty(t, first)

	second, err := svc.ListRouletteMembers("ws-1", "", false)
	require.NoError(t, err)
	assert.Empty(t, second)
	assert.Equal(t, 1, members.calls)
}

func TestRoster_CacheErrorsDegradeToLiveComputation(t *testing.T) {
	members := &stubMembers{members: []*workspace.Member{member("ana", workspace.RoleMember)}}
	authz := &stubAuthz{roulette: map[string]bool{"ana": true}}
	shared := newMemShared()
	shared.getErr = errors.New("redis down")
	shared.setErr = errors.New("redis down")

	svc := newRoster(t, members, nil, authz, shared)
	for i := 0; i < 3; i++ {
		got, err := svc.ListRouletteMembers("ws-1", "", false)
		require.NoError(t, err)
		assert.Equal(t, []string{"ana"}, got)
	}
	assert.Equal(t, 3, members.calls)
}

func TestRoster_CacheKeyIsScoped(t *testing.T) {
	members := &stubMembers{members: []*workspace.Member{
		member("ana", workspace.RoleMember),
		member("owner", workspace.RoleOwner),
	}}
	authz := &stubAuthz{
		roulette:     map[string]bool{"ana": true, "owner": true},
		ownerOrAdmin: map[string]bool{"owner": true},
	}
	shared := newMemShared()
	svc := newRoster(t, members, nil, authz, shared)

	withAdmins, err := svc.ListRouletteMembers("ws-1", "", false)
	require.NoError(t, err)
	skipping, err := svc.ListRouletteMembers("ws-1", "", true)
	require.NoError(t, err)

	assert.Equal(t, []string{"ana", "owner"}, withAdmins)
	assert.Equal(t, []string{"ana"}, skipping)
}
