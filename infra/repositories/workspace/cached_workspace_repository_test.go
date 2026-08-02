package workspace_repository

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/workspace"
)

type fakeSharedState struct {
	mu   sync.Mutex
	data map[string]string
}

func newFakeSharedState() *fakeSharedState {
	return &fakeSharedState{data: make(map[string]string)}
}

func (f *fakeSharedState) SetString(key, value string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = value
	return nil
}
func (f *fakeSharedState) GetString(key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.data[key], nil
}
func (f *fakeSharedState) Del(keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.data, k)
	}
	return nil
}
func (f *fakeSharedState) Exists(key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.data[key]
	return ok, nil
}

func (f *fakeSharedState) SetNX(string, string, time.Duration) (bool, error) { return true, nil }
func (f *fakeSharedState) Incr(string) (int64, error)                        { return 0, nil }
func (f *fakeSharedState) Decr(string) (int64, error)                        { return 0, nil }
func (f *fakeSharedState) IncrWithTTL(string, time.Duration) (int64, error)  { return 0, nil }
func (f *fakeSharedState) TryIncr(string, int64) (bool, error)               { return true, nil }
func (f *fakeSharedState) SAdd(string, ...string) error                      { return nil }
func (f *fakeSharedState) SRem(string, ...string) error                      { return nil }
func (f *fakeSharedState) SMembers(string) ([]string, error)                 { return nil, nil }
func (f *fakeSharedState) Publish(string, []byte) error                      { return nil }
func (f *fakeSharedState) Subscribe(context.Context, string, func([]byte))   {}
func (f *fakeSharedState) HSet(string, string, string) error                 { return nil }
func (f *fakeSharedState) HDel(string, string) error                         { return nil }
func (f *fakeSharedState) HGetAll(string) (map[string]string, error)         { return nil, nil }
func (f *fakeSharedState) HIncrBy(string, string, int64) (int64, error)      { return 0, nil }
func (f *fakeSharedState) IncrBy(string, int64) (int64, error)               { return 0, nil }
func (f *fakeSharedState) DecrBy(string, int64) (int64, error)               { return 0, nil }
func (f *fakeSharedState) TryIncrBy(string, int64, int64) (bool, error)      { return true, nil }
func (f *fakeSharedState) Expire(string, time.Duration) (bool, error)        { return true, nil }

type fakeRepo struct {
	mu                 sync.Mutex
	byKey              map[string]*workspace.Member
	byID               map[string]*workspace.Member
	perms              map[string][]*workspace.Permission
	getMemberCalls     int64
	getMemberByIDCalls int64
	getPermCalls       int64
	addCalls           int64
	updateRoleCalls    int64
	updateRoleIDCalls  int64
	removeCalls        int64
	failNextUpdate     bool
	failNextRemove     bool
	failNextSetPerms   bool
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		byKey: map[string]*workspace.Member{},
		byID:  map[string]*workspace.Member{},
		perms: map[string][]*workspace.Permission{},
	}
}

func memberKey(wsID, userID string) string { return wsID + "|" + userID }

func (f *fakeRepo) seed(m *workspace.Member) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byKey[memberKey(m.WorkspaceID, m.UserID)] = m
	f.byID[m.ID] = m
}

func (f *fakeRepo) GetMember(workspaceID, userID string) (*workspace.Member, error) {
	atomic.AddInt64(&f.getMemberCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.byKey[memberKey(workspaceID, userID)]
	if !ok {
		return nil, nil
	}
	cp := *m
	return &cp, nil
}

func (f *fakeRepo) GetMemberByID(memberID string) (*workspace.Member, error) {
	atomic.AddInt64(&f.getMemberByIDCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.byID[memberID]
	if !ok {
		return nil, workspace.ErrMemberNotFound
	}
	cp := *m
	return &cp, nil
}

func (f *fakeRepo) AddMember(member *workspace.Member) error {
	atomic.AddInt64(&f.addCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byKey[memberKey(member.WorkspaceID, member.UserID)] = member
	f.byID[member.ID] = member
	return nil
}

func (f *fakeRepo) UpdateMemberRole(memberID string, role workspace.Role) error {
	atomic.AddInt64(&f.updateRoleCalls, 1)
	if f.failNextUpdate {
		f.failNextUpdate = false
		return errors.New("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.byID[memberID]; ok {
		m.Role = role
	}
	return nil
}

func (f *fakeRepo) UpdateMemberRingChannels(string, []workspace.RingChannel) error { return nil }

func (f *fakeRepo) UpdateMemberRoleID(memberID string, roleID string) error {
	atomic.AddInt64(&f.updateRoleIDCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.byID[memberID]; ok {
		m.RoleID = roleID
	}
	return nil
}

func (f *fakeRepo) RemoveMember(memberID string) error {
	atomic.AddInt64(&f.removeCalls, 1)
	if f.failNextRemove {
		f.failNextRemove = false
		return errors.New("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.byID[memberID]; ok {
		delete(f.byKey, memberKey(m.WorkspaceID, m.UserID))
		delete(f.byID, memberID)
	}
	return nil
}

func (f *fakeRepo) WithTx(interface{}) workspace.Repository                  { return f }
func (f *fakeRepo) CreateWorkspace(*workspace.Workspace) error               { panic("unused") }
func (f *fakeRepo) GetWorkspaceByID(string) (*workspace.Workspace, error)    { panic("unused") }
func (f *fakeRepo) GetDefaultWorkspace(string) (*workspace.Workspace, error) { panic("unused") }
func (f *fakeRepo) ListWorkspacesByUser(string, string, string) ([]*workspace.Workspace, error) {
	panic("unused")
}
func (f *fakeRepo) ListAllWorkspaces(string, string, int, int) ([]*workspace.Workspace, int64, error) {
	panic("unused")
}
func (f *fakeRepo) ListAllWorkspaceIDs() ([]string, error)                      { panic("unused") }
func (f *fakeRepo) CountMembersByWorkspaceIDs([]string) (map[string]int, error) { panic("unused") }
func (f *fakeRepo) ListMembersPaginated(string, int, int) ([]*workspace.Member, int64, error) {
	panic("unused")
}
func (f *fakeRepo) UpdateWorkspace(*workspace.Workspace) error      { panic("unused") }
func (f *fakeRepo) TransferOwnership(string, string) error          { panic("unused") }
func (f *fakeRepo) DetachUserAuthoredRefs(string) error             { panic("unused") }
func (f *fakeRepo) ListMembers(string) ([]*workspace.Member, error) { panic("unused") }
func (f *fakeRepo) ListAssignableMembers(string, string, bool, []string, bool, string, int, int) ([]*workspace.Member, int64, error) {
	panic("unused")
}
func (f *fakeRepo) ListMemberDepartments(string, []string, []string) (map[string][]workspace.DepartmentRef, error) {
	panic("unused")
}
func (f *fakeRepo) AddPermission(p *workspace.Permission) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.perms[p.MemberID] = append(f.perms[p.MemberID], p)
	return nil
}
func (f *fakeRepo) RemovePermission(memberID string, resource workspace.Resource, action workspace.Action) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur := f.perms[memberID]
	out := cur[:0:0]
	removed := false
	for _, p := range cur {
		if p.Resource == resource && p.Action == action {
			removed = true
			continue
		}
		out = append(out, p)
	}
	if !removed {
		return workspace.ErrPermissionNotFound
	}
	f.perms[memberID] = out
	return nil
}
func (f *fakeRepo) GetPermissions(memberID string) ([]*workspace.Permission, error) {
	atomic.AddInt64(&f.getPermCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	src := f.perms[memberID]
	out := make([]*workspace.Permission, len(src))
	for i, p := range src {
		cp := *p
		out[i] = &cp
	}
	return out, nil
}
func (f *fakeRepo) HasPermission(memberID string, resource workspace.Resource, action workspace.Action) (bool, error) {
	panic("cached repo must answer HasPermission from the grant set, never delegate")
}
func (f *fakeRepo) SetPermissions(memberID string, permissions []*workspace.Permission) error {
	if f.failNextSetPerms {
		f.failNextSetPerms = false
		return errors.New("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.perms[memberID] = append([]*workspace.Permission(nil), permissions...)
	return nil
}
func (f *fakeRepo) CreateInvite(*workspace.Invite) error               { panic("unused") }
func (f *fakeRepo) GetInviteByID(string) (*workspace.Invite, error)    { panic("unused") }
func (f *fakeRepo) GetInviteByToken(string) (*workspace.Invite, error) { panic("unused") }
func (f *fakeRepo) ListInvitesByWorkspace(string) ([]*workspace.Invite, error) {
	panic("unused")
}
func (f *fakeRepo) ListInvitesByEmail(string) ([]*workspace.Invite, error)   { panic("unused") }
func (f *fakeRepo) UpdateInviteStatus(string, workspace.InviteStatus) error  { panic("unused") }
func (f *fakeRepo) PendingInviteExists(string, string) (bool, error)         { panic("unused") }
func (f *fakeRepo) GetWorkspaceIDForResource(string, string) (string, error) { panic("unused") }
func (f *fakeRepo) AssignResource(*workspace.ResourceAssignment) error       { panic("unused") }
func (f *fakeRepo) UnassignResource(string, workspace.Resource, string, string) error {
	panic("unused")
}
func (f *fakeRepo) ListAssignmentsByResource(string, workspace.Resource, string) ([]*workspace.ResourceAssignment, error) {
	panic("unused")
}
func (f *fakeRepo) ListAssignmentsByMember(string, workspace.Resource) ([]*workspace.ResourceAssignment, error) {
	panic("unused")
}
func (f *fakeRepo) IsResourceAssignedToMember(string, workspace.Resource, string, string) (bool, error) {
	panic("unused")
}
func (f *fakeRepo) HasAnyAssignments(string, workspace.Resource, string) (bool, error) {
	panic("unused")
}

func newSeededCache(t *testing.T) (*CachedWorkspaceRepository, *fakeRepo, *fakeSharedState, *workspace.Member) {
	t.Helper()
	repo := newFakeRepo()
	shared := newFakeSharedState()
	m := &workspace.Member{
		ID:          "mem-1",
		WorkspaceID: "ws-1",
		UserID:      "user-1",
		Role:        workspace.RoleMember,
	}
	repo.seed(m)
	cached := NewCachedWorkspaceRepository(repo, shared).(*CachedWorkspaceRepository)
	return cached, repo, shared, m
}

func TestCachedWS_GetMember_CachesAfterFirstRead(t *testing.T) {
	cached, repo, _, _ := newSeededCache(t)

	if m, err := cached.GetMember("ws-1", "user-1"); err != nil || m == nil || m.ID != "mem-1" {
		t.Fatalf("first read: m=%v err=%v", m, err)
	}
	if got := atomic.LoadInt64(&repo.getMemberCalls); got != 1 {
		t.Fatalf("first read should hit inner once, got %d", got)
	}

	for i := 0; i < 10; i++ {
		if m, err := cached.GetMember("ws-1", "user-1"); err != nil || m == nil {
			t.Fatalf("cached read %d: m=%v err=%v", i, m, err)
		}
	}
	if got := atomic.LoadInt64(&repo.getMemberCalls); got != 1 {
		t.Fatalf("cached reads must NOT hit inner; got %d inner calls", got)
	}
}

func TestCachedWS_UpdateMemberRole_InvalidatesCache(t *testing.T) {
	cached, repo, _, _ := newSeededCache(t)

	_, _ = cached.GetMember("ws-1", "user-1")
	if got := atomic.LoadInt64(&repo.getMemberCalls); got != 1 {
		t.Fatalf("seed read: %d", got)
	}

	if err := cached.UpdateMemberRole("mem-1", workspace.RoleAdmin); err != nil {
		t.Fatalf("update: %v", err)
	}

	if m, err := cached.GetMember("ws-1", "user-1"); err != nil || m == nil || m.Role != workspace.RoleAdmin {
		t.Fatalf("post-update read should observe new role: m=%v err=%v", m, err)
	}
	if got := atomic.LoadInt64(&repo.getMemberCalls); got != 2 {
		t.Fatalf("cache should have been invalidated; inner GetMember calls=%d (want 2)", got)
	}
}

func TestCachedWS_UpdateMemberRoleID_InvalidatesCache(t *testing.T) {
	cached, repo, _, _ := newSeededCache(t)

	_, _ = cached.GetMember("ws-1", "user-1")
	if err := cached.UpdateMemberRoleID("mem-1", "role-xyz"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if m, _ := cached.GetMember("ws-1", "user-1"); m == nil || m.RoleID != "role-xyz" {
		t.Fatalf("post-update RoleID = %q", m.RoleID)
	}
	if got := atomic.LoadInt64(&repo.getMemberCalls); got != 2 {
		t.Fatalf("cache should have been invalidated; got %d", got)
	}
}

func TestCachedWS_RemoveMember_InvalidatesCache(t *testing.T) {
	cached, repo, shared, _ := newSeededCache(t)

	_, _ = cached.GetMember("ws-1", "user-1")
	if _, ok := shared.data[memberCacheKey("ws-1", "user-1")]; !ok {
		t.Fatalf("expected cache entry after read")
	}

	if err := cached.RemoveMember("mem-1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := shared.data[memberCacheKey("ws-1", "user-1")]; ok {
		t.Fatalf("cache must be invalidated after RemoveMember")
	}

	m, err := cached.GetMember("ws-1", "user-1")
	if err != nil || m != nil {
		t.Fatalf("after remove: m=%v err=%v (want nil/nil)", m, err)
	}
	_ = repo
	if _, ok := shared.data[memberCacheKey("ws-1", "user-1")]; ok {
		t.Fatalf("nil result must NOT be cached")
	}
}

func TestCachedWS_AddMember_InvalidatesCache(t *testing.T) {
	repo := newFakeRepo()
	shared := newFakeSharedState()
	cached := NewCachedWorkspaceRepository(repo, shared).(*CachedWorkspaceRepository)

	shared.SetString(memberCacheKey("ws-2", "user-2"), `{"id":"old","workspaceId":"ws-2","userId":"user-2"}`, time.Minute)

	newM := &workspace.Member{ID: "mem-2", WorkspaceID: "ws-2", UserID: "user-2", Role: workspace.RoleMember}
	if err := cached.AddMember(newM); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, ok := shared.data[memberCacheKey("ws-2", "user-2")]; ok {
		t.Fatalf("cache must be invalidated after AddMember")
	}

	got, _ := cached.GetMember("ws-2", "user-2")
	if got == nil || got.ID != "mem-2" {
		t.Fatalf("expected fresh member after AddMember, got %v", got)
	}
}

func TestCachedWS_UpdateError_DoesNotInvalidate(t *testing.T) {
	cached, repo, shared, _ := newSeededCache(t)
	_, _ = cached.GetMember("ws-1", "user-1")
	if _, ok := shared.data[memberCacheKey("ws-1", "user-1")]; !ok {
		t.Fatalf("expected cache entry")
	}

	repo.failNextUpdate = true
	if err := cached.UpdateMemberRole("mem-1", workspace.RoleAdmin); err == nil {
		t.Fatalf("expected error from update")
	}

	if _, ok := shared.data[memberCacheKey("ws-1", "user-1")]; !ok {
		t.Fatalf("cache must NOT be invalidated on failed update")
	}
}

func TestCachedWS_NotFoundIsNotCached(t *testing.T) {
	repo := newFakeRepo()
	shared := newFakeSharedState()
	cached := NewCachedWorkspaceRepository(repo, shared).(*CachedWorkspaceRepository)

	m, err := cached.GetMember("ws-x", "user-x")
	if err != nil || m != nil {
		t.Fatalf("nil expected; got m=%v err=%v", m, err)
	}
	if _, ok := shared.data[memberCacheKey("ws-x", "user-x")]; ok {
		t.Fatalf("nil result must NOT be cached")
	}

	_, _ = cached.GetMember("ws-x", "user-x")
	if got := atomic.LoadInt64(&repo.getMemberCalls); got != 2 {
		t.Fatalf("nil-cached path: expected 2 inner calls, got %d", got)
	}
}

func TestCachedWS_NilSharedFallsThroughToInner(t *testing.T) {
	repo := newFakeRepo()
	repo.seed(&workspace.Member{ID: "x", WorkspaceID: "w", UserID: "u", Role: workspace.RoleMember})
	cached := NewCachedWorkspaceRepository(repo, nil).(*CachedWorkspaceRepository)

	for i := 0; i < 5; i++ {
		if m, _ := cached.GetMember("w", "u"); m == nil {
			t.Fatalf("nil shared: GetMember returned nil")
		}
	}
	if got := atomic.LoadInt64(&repo.getMemberCalls); got != 5 {
		t.Fatalf("with nil shared every call must hit inner; got %d", got)
	}
}

func TestCachedWS_EmptyIDsPassThrough(t *testing.T) {
	repo := newFakeRepo()
	shared := newFakeSharedState()
	cached := NewCachedWorkspaceRepository(repo, shared).(*CachedWorkspaceRepository)

	if _, err := cached.GetMember("", "u"); err != nil {
		t.Fatalf("empty workspaceID: %v", err)
	}
	if _, err := cached.GetMember("w", ""); err != nil {
		t.Fatalf("empty userID: %v", err)
	}
	if got := atomic.LoadInt64(&repo.getMemberCalls); got != 2 {
		t.Fatalf("empty IDs must bypass cache; inner calls=%d", got)
	}
	if len(shared.data) != 0 {
		t.Fatalf("empty IDs must NOT populate cache; size=%d", len(shared.data))
	}
}

func seedPerm(memberID string, resource workspace.Resource, action workspace.Action) *workspace.Permission {
	return &workspace.Permission{ID: "p-" + string(resource) + "-" + string(action), MemberID: memberID, Resource: resource, Action: action}
}

// The route access check and the presence/WS check share this repo; both call
// HasPermission for non-admin members. The grant set must be served from one
// cached key so repeats never touch the DB.
func TestCachedWS_HasPermission_CachesGrantSet(t *testing.T) {
	repo := newFakeRepo()
	shared := newFakeSharedState()
	cached := NewCachedWorkspaceRepository(repo, shared).(*CachedWorkspaceRepository)
	_ = repo.AddPermission(seedPerm("mem-1", "dialer", "list_members"))

	ok, err := cached.HasPermission("mem-1", "dialer", "list_members")
	if err != nil || !ok {
		t.Fatalf("first HasPermission: ok=%v err=%v", ok, err)
	}
	if got := atomic.LoadInt64(&repo.getPermCalls); got != 1 {
		t.Fatalf("first HasPermission should load grants once, got %d", got)
	}

	// Repeated permission checks (granted, denied, and full-set reads) all serve
	// from the single cached grant set: no further DB loads.
	for i := 0; i < 10; i++ {
		if ok, _ := cached.HasPermission("mem-1", "dialer", "list_members"); !ok {
			t.Fatalf("cached grant check %d returned false", i)
		}
		if ok, _ := cached.HasPermission("mem-1", "dialer", "delete"); ok {
			t.Fatalf("cached deny check %d returned true", i)
		}
		if _, err := cached.GetPermissions("mem-1"); err != nil {
			t.Fatalf("cached GetPermissions %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt64(&repo.getPermCalls); got != 1 {
		t.Fatalf("cache miss: grants loaded %d times across 31 checks, want 1", got)
	}
}

// A member with no explicit grants is the common case and was the DB-hammering
// path: every route check and every presence broadcast used to COUNT rows. The
// empty grant set must be negatively cached so repeats never touch the DB.
func TestCachedWS_HasPermission_NegativeCachesEmptyGrantSet(t *testing.T) {
	repo := newFakeRepo()
	shared := newFakeSharedState()
	cached := NewCachedWorkspaceRepository(repo, shared).(*CachedWorkspaceRepository)

	for i := 0; i < 10; i++ {
		if ok, err := cached.HasPermission("mem-empty", "dialer", "list_members"); err != nil || ok {
			t.Fatalf("empty-grant check %d: ok=%v err=%v (want false)", i, ok, err)
		}
	}
	if got := atomic.LoadInt64(&repo.getPermCalls); got != 1 {
		t.Fatalf("empty grant set must be cached; loaded %d times across 10 checks, want 1", got)
	}
}

func TestCachedWS_SetPermissions_InvalidatesGrantCache(t *testing.T) {
	repo := newFakeRepo()
	shared := newFakeSharedState()
	cached := NewCachedWorkspaceRepository(repo, shared).(*CachedWorkspaceRepository)
	_ = repo.AddPermission(seedPerm("mem-1", "dialer", "list_members"))

	// Warm the cache, then a fresh grant set must be observed immediately.
	_, _ = cached.HasPermission("mem-1", "dialer", "list_members")
	if _, ok := shared.data[permCacheKey("mem-1")]; !ok {
		t.Fatalf("expected grant cache entry after read")
	}

	if err := cached.SetPermissions("mem-1", []*workspace.Permission{seedPerm("mem-1", "billing", "view")}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, ok := shared.data[permCacheKey("mem-1")]; ok {
		t.Fatalf("grant cache must be invalidated after SetPermissions")
	}

	if ok, _ := cached.HasPermission("mem-1", "dialer", "list_members"); ok {
		t.Fatalf("revoked grant must no longer be present")
	}
	if ok, _ := cached.HasPermission("mem-1", "billing", "view"); !ok {
		t.Fatalf("newly granted permission must be observed after invalidation")
	}
}

func TestCachedWS_AddRemovePermission_InvalidatesGrantCache(t *testing.T) {
	repo := newFakeRepo()
	shared := newFakeSharedState()
	cached := NewCachedWorkspaceRepository(repo, shared).(*CachedWorkspaceRepository)

	// Deny is cached; granting must invalidate so the new grant is seen at once.
	if ok, _ := cached.HasPermission("mem-1", "dialer", "list_members"); ok {
		t.Fatalf("unexpected initial grant")
	}
	if err := cached.AddPermission(seedPerm("mem-1", "dialer", "list_members")); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, ok := shared.data[permCacheKey("mem-1")]; ok {
		t.Fatalf("grant cache must be invalidated after AddPermission")
	}
	if ok, _ := cached.HasPermission("mem-1", "dialer", "list_members"); !ok {
		t.Fatalf("added grant must be observed after invalidation")
	}

	// Revoking must invalidate so the removed grant stops being served.
	if err := cached.RemovePermission("mem-1", "dialer", "list_members"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := shared.data[permCacheKey("mem-1")]; ok {
		t.Fatalf("grant cache must be invalidated after RemovePermission")
	}
	if ok, _ := cached.HasPermission("mem-1", "dialer", "list_members"); ok {
		t.Fatalf("revoked grant must no longer be present")
	}
}

func TestCachedWS_HasPermission_NilSharedAndEmptyIDFallThrough(t *testing.T) {
	// With no shared state every check must hit the inner repo (fail-open to the
	// source of truth, never a silent deny).
	repo := newFakeRepo()
	_ = repo.AddPermission(seedPerm("mem-1", "dialer", "list_members"))
	cached := NewCachedWorkspaceRepository(repo, nil).(*CachedWorkspaceRepository)

	for i := 0; i < 5; i++ {
		if ok, err := cached.HasPermission("mem-1", "dialer", "list_members"); err != nil || !ok {
			t.Fatalf("nil shared HasPermission %d: ok=%v err=%v", i, ok, err)
		}
	}
	if got := atomic.LoadInt64(&repo.getPermCalls); got != 5 {
		t.Fatalf("nil shared must load from inner every call; got %d (want 5)", got)
	}

	// Empty memberID must bypass the cache and never be persisted.
	shared := newFakeSharedState()
	cached2 := NewCachedWorkspaceRepository(repo, shared).(*CachedWorkspaceRepository)
	if _, err := cached2.HasPermission("", "dialer", "list_members"); err != nil {
		t.Fatalf("empty memberID: %v", err)
	}
	if len(shared.data) != 0 {
		t.Fatalf("empty memberID must NOT populate cache; size=%d", len(shared.data))
	}
}

func TestCachedWS_HasPermission_ConcurrentRaceSafe(t *testing.T) {
	repo := newFakeRepo()
	shared := newFakeSharedState()
	cached := NewCachedWorkspaceRepository(repo, shared).(*CachedWorkspaceRepository)
	_ = repo.AddPermission(seedPerm("mem-1", "dialer", "list_members"))

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if ok, err := cached.HasPermission("mem-1", "dialer", "list_members"); err != nil || !ok {
					t.Errorf("concurrent HasPermission: ok=%v err=%v", ok, err)
					return
				}
				// Interleave a grant mutation so reads race against invalidation.
				if n%8 == 0 && j == 50 {
					_ = cached.SetPermissions("mem-1", []*workspace.Permission{seedPerm("mem-1", "dialer", "list_members")})
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestCachedWS_SetPermissionsError_DoesNotInvalidate(t *testing.T) {
	repo := newFakeRepo()
	shared := newFakeSharedState()
	cached := NewCachedWorkspaceRepository(repo, shared).(*CachedWorkspaceRepository)
	_ = repo.AddPermission(seedPerm("mem-1", "dialer", "list_members"))
	_, _ = cached.HasPermission("mem-1", "dialer", "list_members")
	if _, ok := shared.data[permCacheKey("mem-1")]; !ok {
		t.Fatalf("expected grant cache entry")
	}

	repo.failNextSetPerms = true
	if err := cached.SetPermissions("mem-1", nil); err == nil {
		t.Fatalf("expected error from SetPermissions")
	}
	if _, ok := shared.data[permCacheKey("mem-1")]; !ok {
		t.Fatalf("grant cache must NOT be invalidated on failed SetPermissions")
	}
}

func TestCachedWS_ConcurrentReadsRaceSafe(t *testing.T) {
	cached, _, _, _ := newSeededCache(t)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if m, err := cached.GetMember("ws-1", "user-1"); err != nil || m == nil {
					t.Errorf("concurrent read failed: m=%v err=%v", m, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
