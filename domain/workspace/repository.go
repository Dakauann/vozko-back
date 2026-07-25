package workspace

type Repository interface {
	WithTx(tx interface{}) Repository
	CreateWorkspace(ws *Workspace) error
	GetWorkspaceByID(id string) (*Workspace, error)
	GetDefaultWorkspace(ownerID string) (*Workspace, error)
	ListWorkspacesByUser(userID, search, memberEmail string) ([]*Workspace, error)
	ListAllWorkspaces(search, memberEmail string, page, pageSize int) ([]*Workspace, int64, error)
	ListAllWorkspaceIDs() ([]string, error)
	CountMembersByWorkspaceIDs(workspaceIDs []string) (map[string]int, error)
	ListMembersPaginated(workspaceID string, page, pageSize int) ([]*Member, int64, error)
	UpdateWorkspace(ws *Workspace) error
	// TransferOwnership reassigns workspaces.owner_id to newOwnerID. It demotes the
	// workspace to non-default when the new owner already has a default workspace,
	// to avoid colliding on the (owner_id, is_default) unique index.
	TransferOwnership(workspaceID, newOwnerID string) error
	// DetachUserAuthoredRefs clears the FK references that point at a user via
	// "authored by" columns (workspace_invites.inviter_id and the *_access
	// granted_by columns) so the user row can be hard-deleted. Used by account
	// deletion; usually a no-op for ordinary users.
	DetachUserAuthoredRefs(userID string) error

	AddMember(member *Member) error
	GetMember(workspaceID, userID string) (*Member, error)
	GetMemberByID(memberID string) (*Member, error)
	ListMembers(workspaceID string) ([]*Member, error)
	ListAssignableMembers(workspaceID, search string, restrict bool, departmentIDs []string, includeAdmins bool, selfUserID string, page, pageSize int) ([]*Member, int64, error)
	// ListMemberDepartments returns, for each given member id, the departments it
	// belongs to. When restrictToDeptIDs is non-empty the result is limited to
	// those departments (so a scoped caller never sees department names outside
	// their access); nil/empty means no restriction.
	ListMemberDepartments(workspaceID string, memberIDs []string, restrictToDeptIDs []string) (map[string][]DepartmentRef, error)
	UpdateMemberRole(memberID string, role Role) error
	UpdateMemberRoleID(memberID string, roleID string) error
	// UpdateMemberRingChannels sets the member's ring-channel selection (AOR level).
	UpdateMemberRingChannels(memberID string, channels []RingChannel) error
	RemoveMember(memberID string) error

	AddPermission(perm *Permission) error
	RemovePermission(memberID string, resource Resource, action Action) error
	GetPermissions(memberID string) ([]*Permission, error)
	HasPermission(memberID string, resource Resource, action Action) (bool, error)
	SetPermissions(memberID string, permissions []*Permission) error

	CreateInvite(invite *Invite) error
	GetInviteByID(id string) (*Invite, error)
	GetInviteByToken(token string) (*Invite, error)
	ListInvitesByWorkspace(workspaceID string) ([]*Invite, error)
	ListInvitesByEmail(email string) ([]*Invite, error)
	UpdateInviteStatus(inviteID string, status InviteStatus) error
	PendingInviteExists(workspaceID, email string) (bool, error)

	GetWorkspaceIDForResource(resourceTable, resourceID string) (string, error)

	AssignResource(assignment *ResourceAssignment) error
	UnassignResource(workspaceID string, resourceType Resource, resourceID, memberID string) error
	ListAssignmentsByResource(workspaceID string, resourceType Resource, resourceID string) ([]*ResourceAssignment, error)
	ListAssignmentsByMember(memberID string, resourceType Resource) ([]*ResourceAssignment, error)
	IsResourceAssignedToMember(workspaceID string, resourceType Resource, resourceID, memberID string) (bool, error)
	HasAnyAssignments(workspaceID string, resourceType Resource, resourceID string) (bool, error)
}
