package workspace_repository

import (
	"encoding/json"
	"strings"
	"time"

	"vozko/domain/cache"
	"vozko/domain/workspace"
)

const memberCacheTTL = 60 * time.Second

type CachedWorkspaceRepository struct {
	inner  workspace.Repository
	shared cache.SharedState
	ttl    time.Duration
}

func NewCachedWorkspaceRepository(inner workspace.Repository, shared cache.SharedState) workspace.Repository {
	return &CachedWorkspaceRepository{inner: inner, shared: shared, ttl: memberCacheTTL}
}

func memberCacheKey(workspaceID, userID string) string {
	return "ws:member:" + workspaceID + ":" + userID
}

func (r *CachedWorkspaceRepository) invalidateMember(workspaceID, userID string) {
	if r.shared == nil || workspaceID == "" || userID == "" {
		return
	}
	_ = r.shared.Del(memberCacheKey(workspaceID, userID))
}

func (r *CachedWorkspaceRepository) invalidateMemberByID(memberID string) {
	if r.shared == nil || memberID == "" {
		return
	}
	m, err := r.inner.GetMemberByID(memberID)
	if err != nil || m == nil {
		return
	}
	r.invalidateMember(m.WorkspaceID, m.UserID)
}

func permCacheKey(memberID string) string {
	return "ws:perms:" + memberID
}

func (r *CachedWorkspaceRepository) invalidatePermissions(memberID string) {
	mID := strings.TrimSpace(memberID)
	if r.shared == nil || mID == "" {
		return
	}
	_ = r.shared.Del(permCacheKey(mID))
}

// permSet serves a member's full grant set cache-aside. HasPermission and
// GetPermissions both answer from it, so a single key per member is the unit
// of caching and invalidation. Grants are role-independent (role is a separate
// bypass in the access use cases), so only the three permission mutators below
// invalidate this key; a short TTL bounds staleness as a safety net.
func (r *CachedWorkspaceRepository) permSet(memberID string) ([]*workspace.Permission, error) {
	mID := strings.TrimSpace(memberID)
	if mID == "" || r.shared == nil {
		return r.inner.GetPermissions(memberID)
	}

	key := permCacheKey(mID)
	if raw, err := r.shared.GetString(key); err == nil && raw != "" {
		var ps []*workspace.Permission
		if json.Unmarshal([]byte(raw), &ps) == nil {
			return ps, nil
		}
	}

	ps, err := r.inner.GetPermissions(memberID)
	if err != nil {
		return nil, err
	}
	if data, mErr := json.Marshal(ps); mErr == nil {
		_ = r.shared.SetString(key, string(data), r.ttl)
	}
	return ps, nil
}

func (r *CachedWorkspaceRepository) GetMember(workspaceID, userID string) (*workspace.Member, error) {
	wsID := strings.TrimSpace(workspaceID)
	uID := strings.TrimSpace(userID)
	if wsID == "" || uID == "" || r.shared == nil {
		return r.inner.GetMember(workspaceID, userID)
	}

	key := memberCacheKey(wsID, uID)
	if raw, err := r.shared.GetString(key); err == nil && raw != "" {
		var m workspace.Member
		if json.Unmarshal([]byte(raw), &m) == nil && m.ID != "" {
			return &m, nil
		}
	}

	m, err := r.inner.GetMember(workspaceID, userID)
	if err != nil || m == nil {

		return m, err
	}
	if data, mErr := json.Marshal(m); mErr == nil {
		_ = r.shared.SetString(key, string(data), r.ttl)
	}
	return m, nil
}

func (r *CachedWorkspaceRepository) AddMember(member *workspace.Member) error {
	err := r.inner.AddMember(member)
	if err == nil && member != nil {
		r.invalidateMember(member.WorkspaceID, member.UserID)
	}
	return err
}

func (r *CachedWorkspaceRepository) UpdateMemberRole(memberID string, role workspace.Role) error {

	m, _ := r.inner.GetMemberByID(memberID)
	err := r.inner.UpdateMemberRole(memberID, role)
	if err == nil && m != nil {
		r.invalidateMember(m.WorkspaceID, m.UserID)
	}
	return err
}

func (r *CachedWorkspaceRepository) UpdateMemberRoleID(memberID string, roleID string) error {
	m, _ := r.inner.GetMemberByID(memberID)
	err := r.inner.UpdateMemberRoleID(memberID, roleID)
	if err == nil && m != nil {
		r.invalidateMember(m.WorkspaceID, m.UserID)
	}
	return err
}

func (r *CachedWorkspaceRepository) UpdateMemberRingChannels(memberID string, channels []workspace.RingChannel) error {
	m, _ := r.inner.GetMemberByID(memberID)
	err := r.inner.UpdateMemberRingChannels(memberID, channels)
	if err == nil && m != nil {
		r.invalidateMember(m.WorkspaceID, m.UserID)
	}
	return err
}

func (r *CachedWorkspaceRepository) RemoveMember(memberID string) error {
	m, _ := r.inner.GetMemberByID(memberID)
	err := r.inner.RemoveMember(memberID)
	if err == nil && m != nil {
		r.invalidateMember(m.WorkspaceID, m.UserID)
		r.invalidatePermissions(memberID)
	}
	return err
}

func (r *CachedWorkspaceRepository) WithTx(tx interface{}) workspace.Repository {
	return r.inner.WithTx(tx)
}

func (r *CachedWorkspaceRepository) CreateWorkspace(ws *workspace.Workspace) error {
	return r.inner.CreateWorkspace(ws)
}

func (r *CachedWorkspaceRepository) GetWorkspaceByID(id string) (*workspace.Workspace, error) {
	return r.inner.GetWorkspaceByID(id)
}

func (r *CachedWorkspaceRepository) GetDefaultWorkspace(ownerID string) (*workspace.Workspace, error) {
	return r.inner.GetDefaultWorkspace(ownerID)
}

func (r *CachedWorkspaceRepository) ListWorkspacesByUser(userID, search, memberEmail string) ([]*workspace.Workspace, error) {
	return r.inner.ListWorkspacesByUser(userID, search, memberEmail)
}

func (r *CachedWorkspaceRepository) ListAllWorkspaces(search, memberEmail string, page, pageSize int) ([]*workspace.Workspace, int64, error) {
	return r.inner.ListAllWorkspaces(search, memberEmail, page, pageSize)
}

func (r *CachedWorkspaceRepository) ListAllWorkspaceIDs() ([]string, error) {
	return r.inner.ListAllWorkspaceIDs()
}

func (r *CachedWorkspaceRepository) CountMembersByWorkspaceIDs(workspaceIDs []string) (map[string]int, error) {
	return r.inner.CountMembersByWorkspaceIDs(workspaceIDs)
}

func (r *CachedWorkspaceRepository) ListMembersPaginated(workspaceID string, page, pageSize int) ([]*workspace.Member, int64, error) {
	return r.inner.ListMembersPaginated(workspaceID, page, pageSize)
}

func (r *CachedWorkspaceRepository) UpdateWorkspace(ws *workspace.Workspace) error {
	return r.inner.UpdateWorkspace(ws)
}

func (r *CachedWorkspaceRepository) TransferOwnership(workspaceID, newOwnerID string) error {
	return r.inner.TransferOwnership(workspaceID, newOwnerID)
}

func (r *CachedWorkspaceRepository) DetachUserAuthoredRefs(userID string) error {
	return r.inner.DetachUserAuthoredRefs(userID)
}

func (r *CachedWorkspaceRepository) GetMemberByID(memberID string) (*workspace.Member, error) {
	return r.inner.GetMemberByID(memberID)
}

func (r *CachedWorkspaceRepository) ListMembers(workspaceID string) ([]*workspace.Member, error) {
	return r.inner.ListMembers(workspaceID)
}

func (r *CachedWorkspaceRepository) ListAssignableMembers(workspaceID, search string, restrict bool, departmentIDs []string, includeAdmins bool, selfUserID string, page, pageSize int) ([]*workspace.Member, int64, error) {
	return r.inner.ListAssignableMembers(workspaceID, search, restrict, departmentIDs, includeAdmins, selfUserID, page, pageSize)
}

func (r *CachedWorkspaceRepository) ListMemberDepartments(workspaceID string, memberIDs []string, restrictToDeptIDs []string) (map[string][]workspace.DepartmentRef, error) {
	return r.inner.ListMemberDepartments(workspaceID, memberIDs, restrictToDeptIDs)
}

func (r *CachedWorkspaceRepository) AddPermission(perm *workspace.Permission) error {
	err := r.inner.AddPermission(perm)
	if err == nil && perm != nil {
		r.invalidatePermissions(perm.MemberID)
	}
	return err
}

func (r *CachedWorkspaceRepository) RemovePermission(memberID string, resource workspace.Resource, action workspace.Action) error {
	err := r.inner.RemovePermission(memberID, resource, action)
	if err == nil {
		r.invalidatePermissions(memberID)
	}
	return err
}

func (r *CachedWorkspaceRepository) GetPermissions(memberID string) ([]*workspace.Permission, error) {
	return r.permSet(memberID)
}

func (r *CachedWorkspaceRepository) HasPermission(memberID string, resource workspace.Resource, action workspace.Action) (bool, error) {
	ps, err := r.permSet(memberID)
	if err != nil {
		return false, err
	}
	for _, p := range ps {
		if p != nil && p.Resource == resource && p.Action == action {
			return true, nil
		}
	}
	return false, nil
}

func (r *CachedWorkspaceRepository) SetPermissions(memberID string, permissions []*workspace.Permission) error {
	err := r.inner.SetPermissions(memberID, permissions)
	if err == nil {
		r.invalidatePermissions(memberID)
	}
	return err
}

func (r *CachedWorkspaceRepository) CreateInvite(invite *workspace.Invite) error {
	return r.inner.CreateInvite(invite)
}

func (r *CachedWorkspaceRepository) GetInviteByID(id string) (*workspace.Invite, error) {
	return r.inner.GetInviteByID(id)
}

func (r *CachedWorkspaceRepository) GetInviteByToken(token string) (*workspace.Invite, error) {
	return r.inner.GetInviteByToken(token)
}

func (r *CachedWorkspaceRepository) ListInvitesByWorkspace(workspaceID string) ([]*workspace.Invite, error) {
	return r.inner.ListInvitesByWorkspace(workspaceID)
}

func (r *CachedWorkspaceRepository) ListInvitesByEmail(email string) ([]*workspace.Invite, error) {
	return r.inner.ListInvitesByEmail(email)
}

func (r *CachedWorkspaceRepository) UpdateInviteStatus(inviteID string, status workspace.InviteStatus) error {
	return r.inner.UpdateInviteStatus(inviteID, status)
}

func (r *CachedWorkspaceRepository) PendingInviteExists(workspaceID, email string) (bool, error) {
	return r.inner.PendingInviteExists(workspaceID, email)
}

func (r *CachedWorkspaceRepository) GetWorkspaceIDForResource(resourceTable, resourceID string) (string, error) {
	return r.inner.GetWorkspaceIDForResource(resourceTable, resourceID)
}

func (r *CachedWorkspaceRepository) AssignResource(assignment *workspace.ResourceAssignment) error {
	return r.inner.AssignResource(assignment)
}

func (r *CachedWorkspaceRepository) UnassignResource(workspaceID string, resourceType workspace.Resource, resourceID, memberID string) error {
	return r.inner.UnassignResource(workspaceID, resourceType, resourceID, memberID)
}

func (r *CachedWorkspaceRepository) ListAssignmentsByResource(workspaceID string, resourceType workspace.Resource, resourceID string) ([]*workspace.ResourceAssignment, error) {
	return r.inner.ListAssignmentsByResource(workspaceID, resourceType, resourceID)
}

func (r *CachedWorkspaceRepository) ListAssignmentsByMember(memberID string, resourceType workspace.Resource) ([]*workspace.ResourceAssignment, error) {
	return r.inner.ListAssignmentsByMember(memberID, resourceType)
}

func (r *CachedWorkspaceRepository) IsResourceAssignedToMember(workspaceID string, resourceType workspace.Resource, resourceID, memberID string) (bool, error) {
	return r.inner.IsResourceAssignedToMember(workspaceID, resourceType, resourceID, memberID)
}

func (r *CachedWorkspaceRepository) HasAnyAssignments(workspaceID string, resourceType workspace.Resource, resourceID string) (bool, error) {
	return r.inner.HasAnyAssignments(workspaceID, resourceType, resourceID)
}
